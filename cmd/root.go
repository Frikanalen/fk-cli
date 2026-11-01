package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github/frikanalen/fk-cli/fk-client"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// version is stamped in at link time by release builds (and by the Makefile)
// with -ldflags "-X github/frikanalen/fk-cli/cmd.version=...". Plain `go build`
// leaves it at "dev".
var version = "dev"

// globalOptions are the flags every command takes.
type globalOptions struct {
	env     string
	verbose bool
	noColor bool
}

// newRootCmd builds the whole command tree. It is built afresh for every run
// so that flag values never leak from one run to the next, which is what lets
// tests drive the CLI end to end.
func newRootCmd() *cobra.Command {
	opts := &globalOptions{}

	root := &cobra.Command{
		Use:     "fk",
		Short:   "Work with Frikanalen from the command line",
		Version: version,
		// Errors are printed once, by run, in one format; a runtime failure
		// is not a reason to show the usage text.
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return opts.apply()
		},
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError{err.Error()}
	})

	flags := root.PersistentFlags()
	flags.StringVar(&opts.env, "env", "", "Environment to use for this command only (default: $FK_ENV, then the active one)")
	flags.BoolVarP(&opts.verbose, "verbose", "v", false, "Log what is going on, to stderr")
	flags.BoolVar(&opts.noColor, "no-color", false, "Disable colour (also: $NO_COLOR)")
	_ = root.RegisterFlagCompletionFunc("env", completeEnvironments)

	root.AddCommand(
		newEnvCmd(),
		newAuthCmd(),
		newOrgCmd(),
		newSeriesCmd(),
		newVideoCmd(),
		newArchiveCmd(),
		newDevCmd(),
	)
	return root
}

// apply puts the global flags into effect, before any command runs.
func (o *globalOptions) apply() error {
	if o.verbose {
		log.SetLevel(log.DebugLevel)
	}
	if o.noColor {
		_ = os.Setenv("NO_COLOR", "1")
	}
	if err := loadConfig(); err != nil {
		return err
	}

	env := o.env
	if env == "" {
		env = os.Getenv("FK_ENV")
	}
	if err := fk.OverrideEnvironment(env); err != nil {
		return usageError{err.Error()}
	}
	log.Debugf("using environment %s (%s)", fk.CurrentEnvironment(), fk.APIURL())
	return nil
}

// loadConfig reads ~/.frikanalen.yaml, creating it on first use.
func loadConfig() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("finding home directory: %w", err)
	}
	configFile := filepath.Join(home, ".frikanalen.yaml")

	// Older versions stored a single api/token pair; fold that into the
	// environments layout before viper reads the file.
	migrated, err := fk.MigrateLegacyConfig(configFile)
	if err != nil {
		return fmt.Errorf("migrating %s: %w", configFile, err)
	}
	if migrated {
		log.Warnf("migrated %s to per-environment configuration", configFile)
	}

	viper.SetConfigFile(configFile)
	viper.SetConfigType("yaml")
	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("reading %s: %w", configFile, err)
		}
		viper.Set("environment", fk.DefaultEnvironment)
		if err := viper.WriteConfigAs(configFile); err != nil {
			return fmt.Errorf("writing %s: %w", configFile, err)
		}
		log.Debugf("created %s", configFile)
		return nil
	}
	log.Debugf("loaded %s", configFile)
	return nil
}

// run executes the CLI with the given arguments and streams, and returns the
// process exit code.
func run(argv []string, stdout, stderr io.Writer) int {
	viper.Reset()
	_ = fk.OverrideEnvironment("")
	log.SetOutput(stderr)
	log.SetFormatter(plainFormatter{})
	log.SetLevel(log.WarnLevel)

	root := newRootCmd()
	root.SetArgs(argv)
	root.SetOut(stdout)
	root.SetErr(stderr)

	cmd, err := root.ExecuteC()
	if err != nil {
		return reportError(stderr, cmd, err)
	}
	return exitOK
}

// Execute runs the CLI against the process's own arguments and streams, and
// returns the exit code for main to exit with.
func Execute() int {
	return run(os.Args[1:], os.Stdout, os.Stderr)
}

// plainFormatter prints log entries as "fk: warning: ..." lines, without
// logrus's timestamps and key=value framing.
type plainFormatter struct{}

func (plainFormatter) Format(entry *log.Entry) ([]byte, error) {
	switch entry.Level {
	case log.DebugLevel, log.TraceLevel:
		return []byte("debug: " + entry.Message + "\n"), nil
	case log.InfoLevel:
		return []byte(entry.Message + "\n"), nil
	default:
		return []byte("fk: " + entry.Level.String() + ": " + entry.Message + "\n"), nil
	}
}

// completeEnvironments offers environment names to shell completion.
func completeEnvironments(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	if err := loadConfig(); err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var names []string
	for _, env := range fk.KnownEnvironments() {
		names = append(names, env.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

package cmd

import (
	"fmt"
	"os"

	"github/frikanalen/fk-cli/fk-client"

	"github.com/jedib0t/go-pretty/v6/table"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Show or switch the API environment",
	Long: "Environments are named servers -- local, staging and prod are built in.\n" +
		"Each keeps its own auth token, so switching back and forth does not mean\n" +
		"logging in again. With no arguments, prints the active environment.",
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		name := fk.CurrentEnvironment()
		fmt.Printf("%s\t%s\n", name, fk.EnvironmentAPI(name))
		warnIfAPIOverridden()
	},
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available environments",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		current := fk.CurrentEnvironment()

		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.AppendHeader(table.Row{"", "name", "api", "logged in"})

		for _, env := range fk.KnownEnvironments() {
			active := ""
			if env.Name == current {
				active = "*"
			}
			loggedIn := "no"
			if env.LoggedIn {
				loggedIn = "yes"
			}
			t.AppendRow(table.Row{active, env.Name, env.API, loggedIn})
		}

		t.SetStyle(table.StyleColoredBlackOnRedWhite)
		t.Render()

		warnIfAPIOverridden()
	},
}

var envUseCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Switch to an environment",
	Long: "Switch to an environment, writing its API URL into the configuration\n" +
		"file. Pass --api to define an environment beyond the built-in ones, or\n" +
		"to point a built-in one somewhere else.",
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]

		api, err := cmd.Flags().GetString("api")
		if err != nil {
			log.Fatal(err)
		}

		if err := fk.UseEnvironment(name, api); err != nil {
			log.Fatal(err)
		}

		log.Infof("now using %s (%s)", name, fk.EnvironmentAPI(name))
		if fk.StoredToken() == "" {
			log.Infoln("no token stored for this environment; run fk login")
		}
		warnIfAPIOverridden()
	},
}

// warnIfAPIOverridden points out that $FK_API is winning over whatever the
// configuration says, which would otherwise be a confusing thing to debug.
func warnIfAPIOverridden() {
	if api := os.Getenv("FK_API"); api != "" {
		log.Warnf("$FK_API is set; requests go to %s regardless of environment", api)
	}
}

func init() {
	rootCmd.AddCommand(envCmd)
	envCmd.AddCommand(envListCmd)
	envCmd.AddCommand(envUseCmd)

	envUseCmd.Flags().String("api", "", "API base URL to define this environment with")
}

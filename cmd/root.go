package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// version is stamped in at link time by release builds (and by the Makefile)
// with -ldflags "-X github/frikanalen/fk-cli/cmd.version=...". Plain `go build`
// leaves it at "dev".
var version = "dev"

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:     "fk",
	Short:   "Frikanalen command-line interface",
	Version: version,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {}

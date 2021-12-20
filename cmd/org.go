package cmd

import (
	"github.com/spf13/cobra"
)

// orgCmd represents the org command
var orgCmd = &cobra.Command{
	Use:   "org",
	Short: "organization management",
}

func init() {
	rootCmd.AddCommand(orgCmd)
}

package cmd

import (
	"github.com/spf13/cobra"
)

// seriesCmd represents the series command
var seriesCmd = &cobra.Command{
	Use:   "series",
	Short: "Series management",
}

func init() {
	rootCmd.AddCommand(seriesCmd)
}

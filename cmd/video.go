package cmd

import (
	"github.com/spf13/cobra"
)

var videoCmd = &cobra.Command{
	Use:   "video",
	Short: "Manage videos",
}

func init() {
	rootCmd.AddCommand(videoCmd)
}

/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"github.com/spf13/cobra"
)

// mediaCmd represents the media command
var mediaCmd = &cobra.Command{
	Use:   "media",
	Short: "Manage media",
}

func init() {
	rootCmd.AddCommand(mediaCmd)
}

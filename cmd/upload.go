/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"github/frikanalen/fk-cli/fk-client"
	"log"

	"github.com/spf13/cobra"
)

// uploadCmd represents the upload command
var uploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "Upload video",
	Run: func(cmd *cobra.Command, args []string) {
		session, err := fk.Open()
		if err != nil {
			log.Fatal(err)
			return
		}
		log.Println(session.Upload(cmd.Flag("file").Value.String()))
	},
}

func init() {
	rootCmd.AddCommand(uploadCmd)
	uploadCmd.Flags().Float32("generateDummy", 0.0, "Generate an n-second test video (for developer use)")
	uploadCmd.Flags().StringP("file", "f", "", "Path to file to upload")
	uploadCmd.Flags().StringP("title", "t", "", "Title of video")
	uploadCmd.MarkFlagRequired("title")
}

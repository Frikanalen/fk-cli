/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"github/frikanalen/fk-cli/fk-client"

	log "github.com/sirupsen/logrus"

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
		fileSpec := cmd.Flag("file").Value.String()
		log.Infoln("Uploading file", fileSpec)
		upload, err := session.Upload(fileSpec)
		if err != nil {
			log.Fatal(err)
			return
		}
		fmt.Println(upload.MediaId)
	},
}

func init() {
	mediaCmd.AddCommand(uploadCmd)
	uploadCmd.Flags().StringP("file", "f", "", "Path to file to upload")
	_ = uploadCmd.MarkFlagRequired("file")
}

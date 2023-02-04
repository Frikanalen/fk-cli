/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github/frikanalen/fk-cli/fk-client"
	"os"

	log "github.com/sirupsen/logrus"

	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate test user against the API",
	Long:  `Stores a session ID in your configuration file. Currently hardcoded to dev admin user`,
	Run: func(cmd *cobra.Command, args []string) {
		client, err := fk.Open()
		if err != nil {
			log.Fatalln("could not open session, %w", err)
			os.Exit(1)
		}
		err = client.Login("dev-admin@frikanalen.no", "dev-admin")
		if err != nil {
			log.Fatalln("could not login, %w", err)
			os.Exit(1)
		}
		log.Infoln("login successful")
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// loginCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// loginCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

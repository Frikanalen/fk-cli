/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"github/frikanalen/fk-cli/fk-client"

	log "github.com/sirupsen/logrus"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate against the API",
	Long:  `Stores a session ID in your configuration file.`,
	Run: func(cmd *cobra.Command, args []string) {
		session := fk.FrikanalenSession{}
		sessionID, err := session.Login("dev-admin@frikanalen.no", "dev-admin")
		if err != nil {
			log.Fatalf("could not log in %w", err)
			return
		}
		viper.Set("sessionID", sessionID)
		viper.WriteConfig()
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

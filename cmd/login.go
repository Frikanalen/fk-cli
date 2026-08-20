package cmd

import (
	"context"
	"fmt"
	"os"

	"github/frikanalen/fk-cli/fk-client"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate against the API",
	Long:  `Obtains an auth token and stores it in your configuration file.`,
	Run: func(cmd *cobra.Command, args []string) {
		client, err := fk.Open()
		if err != nil {
			log.Fatalln("could not open session:", err)
		}

		email, err := cmd.Flags().GetString("email")
		if err != nil {
			log.Fatalln(err)
		}

		password, err := cmd.Flags().GetString("password")
		if err != nil {
			log.Fatalln(err)
		}
		if password == "" {
			fmt.Print("Password: ")
			passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println()
			if err != nil {
				log.Fatalln("could not read password:", err)
			}
			password = string(passwordBytes)
		}

		if err := client.Login(context.Background(), email, password); err != nil {
			log.Fatalln("could not login:", err)
		}

		viper.Set("token", client.Token())
		if err := viper.WriteConfig(); err != nil {
			log.Fatalln("could not save token:", err)
		}
		log.Infoln("login successful")
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)

	loginCmd.Flags().StringP("email", "e", "", "Email address")
	_ = loginCmd.MarkFlagRequired("email")
	loginCmd.Flags().StringP("password", "p", "", "Password (omit to be prompted)")
}

/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"context"
	"github/frikanalen/fk-cli/fk-client"
	"os"

	"github.com/jedib0t/go-pretty/v6/table"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func checkErr(err error) {
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
}

func getUserID() *int {
	client, err := fk.Open()
	checkErr(err)

	userResponse, err := client.UserProfile(context.Background())
	checkErr(err)

	userProfile, err := fk.ParseUserProfileResponse(userResponse)
	checkErr(err)

	return (*userProfile.JSON200).User.Id
}

func getUserOrganizations() []fk.Organization {
	client, err := fk.Open()
	checkErr(err)

	userId := getUserID()

	orgResponse, err := client.GetOrganizations(context.Background(), &fk.GetOrganizationsParams{Editor: userId})
	checkErr(err)

	orgData, err := fk.ParseGetOrganizationsResponse(orgResponse)
	checkErr(err)

	return *orgData.JSON200.Rows
}

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List organizations associated with user",
	Run: func(cmd *cobra.Command, args []string) {
		organizations := getUserOrganizations()

		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.AppendHeader(table.Row{"active", "org id", "name"})

		for _, org := range organizations {
			t.AppendRow(table.Row{"", *org.Id, *org.Name})
		}

		t.SetStyle(table.StyleColoredBlackOnRedWhite)
		t.Render()
	},
}

func init() {
	orgCmd.AddCommand(listCmd)
}

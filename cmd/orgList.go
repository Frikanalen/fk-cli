package cmd

import (
	"context"
	"os"

	"github/frikanalen/fk-cli/fk-client"

	"github.com/jedib0t/go-pretty/v6/table"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func checkErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List organizations associated with user",
	Run: func(cmd *cobra.Command, args []string) {
		client, err := fk.Open()
		checkErr(err)

		user, err := client.Profile(context.Background())
		checkErr(err)

		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.AppendHeader(table.Row{"role", "org id", "name"})

		for _, org := range user.EditorOf {
			t.AppendRow(table.Row{"editor", org.Id, org.Name})
		}
		for _, org := range user.MemberOf {
			t.AppendRow(table.Row{"member", org.Id, org.Name})
		}

		t.SetStyle(table.StyleColoredBlackOnRedWhite)
		t.Render()
	},
}

func init() {
	orgCmd.AddCommand(listCmd)
}

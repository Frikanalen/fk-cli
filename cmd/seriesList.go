package cmd

import (
	"context"
	"os"
	"strconv"

	"github/frikanalen/fk-cli/fk-client"

	"github.com/jedib0t/go-pretty/v6/table"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// seriesListCmd represents the series list command
var seriesListCmd = &cobra.Command{
	Use:   "list <organization-id>",
	Short: "List an organization's series",
	Long: "List the series belonging to an organization, with the IDs to pass\n" +
		"to \"video create --series-id\". Organization IDs come from\n" +
		"\"fk org list\".",
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		orgId, err := strconv.Atoi(args[0])
		if err != nil {
			log.Fatalf("organization ID must be a number, got %q", args[0])
		}

		client, err := fk.Open()
		checkErr(err)

		series, err := client.ListSeries(context.Background(), orgId)
		checkErr(err)

		t := table.NewWriter()
		t.SetOutputMirror(os.Stdout)
		t.AppendHeader(table.Row{"series id", "name"})

		for _, s := range series {
			t.AppendRow(table.Row{s.Id, s.Name})
		}

		t.SetStyle(table.StyleColoredBlackOnRedWhite)
		t.Render()
	},
}

func init() {
	seriesCmd.AddCommand(seriesListCmd)
}

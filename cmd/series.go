package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github/frikanalen/fk-cli/fk-client"

	"github.com/spf13/cobra"
)

func newSeriesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "series",
		Short: "Work with series",
	}
	cmd.AddCommand(newSeriesListCmd())
	return cmd
}

func newSeriesListCmd() *cobra.Command {
	var out output
	var orgId int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List an organization's series",
		Long: "List the series belonging to an organization, with the IDs to pass to\n" +
			"\"fk video create --series\". --org can be left out when you belong to\n" +
			"just one organization.",
		Example: "  fk series list\n" +
			"  fk series list --org 7",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := fk.Open()
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("org") {
				if orgId, err = onlyOrg(cmd.Context(), client); err != nil {
					return err
				}
			}

			series, err := client.ListSeries(cmd.Context(), orgId)
			if err != nil {
				return err
			}
			if series == nil {
				series = []fk.Series{}
			}
			if out.json {
				return printJSON(cmd.OutOrStdout(), series)
			}

			rows := make([][]any, 0, len(series))
			for _, s := range series {
				rows = append(rows, []any{s.Id, s.Name})
			}
			printTable(cmd.OutOrStdout(), []string{"id", "name"}, rows)
			return nil
		},
	}
	cmd.Flags().IntVar(&orgId, "org", 0, "Organization ID (see fk org list)")
	out.addJSONFlag(cmd)
	return cmd
}

// onlyOrg returns the one organization the user belongs to, or explains that
// --org is needed to choose between several.
func onlyOrg(ctx context.Context, client *fk.Client) (int, error) {
	user, err := client.Profile(ctx)
	if err != nil {
		return 0, err
	}
	seen := map[int]string{}
	for _, org := range append(append([]fk.SimpleOrg{}, user.EditorOf...), user.MemberOf...) {
		seen[org.Id] = org.Name
	}
	switch len(seen) {
	case 1:
		for id := range seen {
			return id, nil
		}
	case 0:
		return 0, fmt.Errorf("you do not belong to any organization")
	}

	ids := make([]int, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	choices := make([]string, len(ids))
	for i, id := range ids {
		choices[i] = fmt.Sprintf("%d (%s)", id, seen[id])
	}
	return 0, usageErrorf("you belong to several organizations; pick one with --org: %s", strings.Join(choices, ", "))
}

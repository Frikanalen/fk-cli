package cmd

import (
	"github/frikanalen/fk-cli/fk-client"

	"github.com/spf13/cobra"
)

func newOrgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Work with organizations",
	}
	cmd.AddCommand(newOrgListCmd())
	return cmd
}

// orgView is how an organization appears in --json output.
type orgView struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

func newOrgListCmd() *cobra.Command {
	var out output
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the organizations you belong to",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := fk.Open()
			if err != nil {
				return err
			}
			user, err := client.Profile(cmd.Context())
			if err != nil {
				return err
			}

			views := []orgView{}
			for _, org := range user.EditorOf {
				views = append(views, orgView{Id: org.Id, Name: org.Name, Role: "editor"})
			}
			for _, org := range user.MemberOf {
				views = append(views, orgView{Id: org.Id, Name: org.Name, Role: "member"})
			}
			if out.json {
				return printJSON(cmd.OutOrStdout(), views)
			}

			rows := make([][]any, 0, len(views))
			for _, v := range views {
				rows = append(rows, []any{v.Id, v.Name, v.Role})
			}
			printTable(cmd.OutOrStdout(), []string{"id", "name", "role"}, rows)
			return nil
		},
	}
	out.addJSONFlag(cmd)
	return cmd
}

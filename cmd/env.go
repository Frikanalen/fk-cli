package cmd

import (
	"fmt"

	"github/frikanalen/fk-cli/fk-client"

	"github.com/spf13/cobra"
)

// environmentView is how an environment appears in --json output.
type environmentView struct {
	Name     string `json:"name"`
	API      string `json:"api"`
	Active   bool   `json:"active"`
	LoggedIn bool   `json:"logged_in"`
}

func newEnvCmd() *cobra.Command {
	var out output
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Show the active environment",
		Long: "Environments are named servers: local, staging and prod are built in.\n" +
			"Each keeps its own auth token, so switching back and forth does not mean\n" +
			"logging in again. With no subcommand, prints the active environment.\n" +
			"\n" +
			"--env NAME (or $FK_ENV) picks an environment for a single command\n" +
			"without changing the active one.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name := fk.CurrentEnvironment()
			view := environmentView{
				Name:     name,
				API:      fk.EnvironmentAPI(name),
				Active:   true,
				LoggedIn: fk.StoredToken() != "",
			}
			if out.json {
				return printJSON(cmd.OutOrStdout(), view)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", view.Name, view.API)
			return nil
		},
	}
	out.addJSONFlag(cmd)
	cmd.AddCommand(newEnvListCmd(), newEnvUseCmd())
	return cmd
}

func newEnvListCmd() *cobra.Command {
	var out output
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List environments",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			current := fk.CurrentEnvironment()
			views := []environmentView{}
			for _, env := range fk.KnownEnvironments() {
				views = append(views, environmentView{
					Name:     env.Name,
					API:      env.API,
					Active:   env.Name == current,
					LoggedIn: env.LoggedIn,
				})
			}
			if out.json {
				return printJSON(cmd.OutOrStdout(), views)
			}

			rows := make([][]any, 0, len(views))
			for _, v := range views {
				active := ""
				if v.Active {
					active = "*"
				}
				rows = append(rows, []any{active, v.Name, v.API, yesNo(v.LoggedIn)})
			}
			printTable(cmd.OutOrStdout(), []string{"active", "name", "api", "logged_in"}, rows)
			return nil
		},
	}
	out.addJSONFlag(cmd)
	return cmd
}

func newEnvUseCmd() *cobra.Command {
	var api string
	cmd := &cobra.Command{
		Use:   "use <name>",
		Short: "Switch the active environment",
		Long: "Switch to an environment, writing the choice into the configuration\n" +
			"file. Pass --api to define an environment beyond the built-in ones, or\n" +
			"to point a built-in one somewhere else.",
		Example: "  fk env use staging\n" +
			"  fk env use mine --api http://192.168.1.10:8000",
		Args:              args(1, 1),
		ValidArgsFunction: completeEnvironments,
		RunE: func(cmd *cobra.Command, argv []string) error {
			name := argv[0]
			if err := fk.UseEnvironment(name, api); err != nil {
				return err
			}

			stderr := cmd.ErrOrStderr()
			fmt.Fprintf(stderr, "Now using %s (%s)\n", name, fk.EnvironmentAPI(name))
			if fk.StoredToken() == "" {
				fmt.Fprintln(stderr, "Not logged in here yet; run fk auth login")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&api, "api", "", "API base URL to define this environment with")
	return cmd
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

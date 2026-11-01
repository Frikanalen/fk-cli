package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github/frikanalen/fk-cli/fk-client"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Log in and out",
	}
	cmd.AddCommand(newAuthLoginCmd(), newAuthStatusCmd(), newAuthLogoutCmd())
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	var email string
	var passwordStdin bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to the active environment",
		Long: "Obtain an auth token and store it for the active environment.\n" +
			"\n" +
			"The password is prompted for on a terminal. To log in from a script,\n" +
			"pipe it in with --password-stdin; it is never taken as a flag value,\n" +
			"where it would end up in shell history and the process list.",
		Example: "  fk auth login --email you@example.com\n" +
			"  pass show frikanalen | fk auth login --email you@example.com --password-stdin",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if email == "" {
				return usageErrorf("--email is required")
			}
			password, err := readPassword(cmd, passwordStdin)
			if err != nil {
				return err
			}

			client, err := fk.Open()
			if err != nil {
				return err
			}
			if err := client.Login(cmd.Context(), email, password); err != nil {
				return fmt.Errorf("logging in: %w", err)
			}
			if err := fk.SaveToken(client.Token()); err != nil {
				return fmt.Errorf("saving token: %w", err)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Logged in to %s as %s\n", fk.CurrentEnvironment(), email)
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "Email address to log in with")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "Read the password from stdin")
	return cmd
}

// readPassword takes the password from stdin when asked to, and otherwise
// prompts for it, which needs a terminal.
func readPassword(cmd *cobra.Command, fromStdin bool) (string, error) {
	if fromStdin {
		line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("reading password: %w", err)
		}
		password := strings.TrimRight(line, "\r\n")
		if password == "" {
			return "", usageErrorf("--password-stdin got an empty password")
		}
		return password, nil
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", usageErrorf("stdin is not a terminal; pass --password-stdin to read the password from it")
	}
	fmt.Fprint(cmd.ErrOrStderr(), "Password: ")
	password, err := term.ReadPassword(fd)
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	return string(password), nil
}

// authStatusView is what "auth status --json" prints.
type authStatusView struct {
	Environment string `json:"environment"`
	API         string `json:"api"`
	LoggedIn    bool   `json:"logged_in"`
	Email       string `json:"email,omitempty"`
}

func newAuthStatusCmd() *cobra.Command {
	var out output
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show who you are logged in as",
		Long: "Check the stored token against the server and print who it belongs\n" +
			"to. Exits 1 when not logged in.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			view := authStatusView{Environment: fk.CurrentEnvironment(), API: fk.APIURL()}
			if fk.StoredToken() != "" {
				client, err := fk.Open()
				if err != nil {
					return err
				}
				user, err := client.Profile(cmd.Context())
				var apiErr *fk.APIError
				switch {
				case errors.As(err, &apiErr) && apiErr.Status == 401:
					// A token the server no longer accepts is the same as none.
				case err != nil:
					return err
				default:
					view.LoggedIn = true
					view.Email = user.Email
				}
			}

			if out.json {
				if err := printJSON(cmd.OutOrStdout(), view); err != nil {
					return err
				}
			} else if view.LoggedIn {
				fmt.Fprintf(cmd.OutOrStdout(), "Logged in to %s (%s) as %s\n", view.Environment, view.API, view.Email)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Not logged in to %s (%s)\n", view.Environment, view.API)
			}
			if !view.LoggedIn {
				return errSilentFailure
			}
			return nil
		},
	}
	out.addJSONFlag(cmd)
	return cmd
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the token stored for the active environment",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := fk.ClearToken(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Logged out of %s\n", fk.CurrentEnvironment())
			return nil
		},
	}
}

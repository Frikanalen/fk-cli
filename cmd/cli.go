package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Exit codes. Scripts can tell a mistyped command from one that ran and failed.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// usageError is a command line that could not be understood: wrong number of
// arguments, a malformed ID, a missing or unknown flag. It exits with
// exitUsage rather than exitFailure.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usageErrorf(format string, args ...any) error {
	return usageError{fmt.Sprintf(format, args...)}
}

// hintError pairs a failure with the command that picks up where it left off,
// printed as "hint: <cmd>" under the error.
type hintError struct {
	err  error
	hint string
}

func (h hintError) Error() string { return h.err.Error() }
func (h hintError) Unwrap() error { return h.err }

func withHint(err error, format string, args ...any) error {
	return hintError{err: err, hint: fmt.Sprintf(format, args...)}
}

// errSilentFailure exits with exitFailure without printing anything more: the
// command has already said what there is to say (e.g. "video status" reporting
// a failed ingest on stdout).
var errSilentFailure = errors.New("")

// isCobraUsageError recognises the errors cobra raises itself, for which it
// offers no type to match on.
func isCobraUsageError(err error) bool {
	msg := err.Error()
	for _, prefix := range []string{
		"unknown command",
		"unknown flag",
		"unknown shorthand flag",
		"required flag(s)",
		"invalid argument",
		"flag needs an argument",
		"bad flag syntax",
		"if any flags in the group",
	} {
		if strings.HasPrefix(msg, prefix) {
			return true
		}
	}
	return false
}

// reportError prints err the one way every command reports errors, and
// returns the exit code it calls for.
func reportError(w io.Writer, cmd *cobra.Command, err error) int {
	if errors.Is(err, errSilentFailure) {
		return exitFailure
	}

	var usage usageError
	if errors.As(err, &usage) || isCobraUsageError(err) {
		fmt.Fprintf(w, "fk: %s\n", err)
		path := "fk"
		if cmd != nil {
			path = cmd.CommandPath()
		}
		fmt.Fprintf(w, "Run '%s --help' for usage.\n", path)
		return exitUsage
	}

	fmt.Fprintf(w, "fk: %s\n", err)
	var hint hintError
	if errors.As(err, &hint) {
		fmt.Fprintf(w, "hint: %s\n", hint.hint)
	}
	return exitFailure
}

// args declares the positional arguments a command takes, by the names its
// Use line gives them, and reports a mismatch as a usage error that names
// what was expected.
func args(min, max int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, got []string) error {
		if len(got) >= min && (max < 0 || len(got) <= max) {
			return nil
		}
		want := strings.TrimSpace(strings.TrimPrefix(cmd.Use, cmd.Name()))
		// reportError already says "fk: ", so name the command without it.
		name := strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" ")
		switch {
		case max == 0:
			return usageErrorf("%s takes no arguments, got %q", name, strings.Join(got, " "))
		case len(got) < min:
			return usageErrorf("%s expects %s", name, want)
		default:
			return usageErrorf("%s expects %s, got %d arguments", name, want, len(got))
		}
	}
}

var noArgs = args(0, 0)

// parseID reads a positive integer ID from the command line.
func parseID(raw, what string) (int, error) {
	id, err := strconv.Atoi(raw)
	if err != nil || id < 1 {
		return 0, usageErrorf("%s must be a positive number, got %q", what, raw)
	}
	return id, nil
}

// output is how a command's result reaches stdout: as JSON when --json is
// given, otherwise as text for a person.
type output struct {
	json bool
}

// addJSONFlag gives a command that returns data its --json flag.
func (o *output) addJSONFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&o.json, "json", false, "Print the result as JSON")
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// isTerminal reports whether w is an interactive terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// colorEnabled follows the NO_COLOR convention (https://no-color.org);
// --no-color sets it for the run.
func colorEnabled() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return os.Getenv("TERM") != "dumb"
}

// printTable writes rows under headers: boxed and coloured on a terminal,
// tab-separated with a header line otherwise, so it pipes into cut, awk and
// grep without escape codes in the way.
func printTable(w io.Writer, headers []string, rows [][]any) {
	if !isTerminal(w) {
		fmt.Fprintln(w, strings.Join(headers, "\t"))
		for _, row := range rows {
			cells := make([]string, len(row))
			for i, cell := range row {
				cells[i] = fmt.Sprint(cell)
			}
			fmt.Fprintln(w, strings.Join(cells, "\t"))
		}
		return
	}

	t := table.NewWriter()
	t.SetOutputMirror(w)
	header := make(table.Row, len(headers))
	for i, h := range headers {
		header[i] = h
	}
	t.AppendHeader(header)
	for _, row := range rows {
		t.AppendRow(table.Row(row))
	}
	if colorEnabled() {
		t.SetStyle(table.StyleColoredBlackOnRedWhite)
	} else {
		t.SetStyle(table.StyleLight)
	}
	t.Render()
}

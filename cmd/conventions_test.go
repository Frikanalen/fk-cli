package cmd

import (
	"strings"
	"testing"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// These tests hold the command tree to the conventions in the README's
// "Command-line conventions" section, so a new command cannot quietly
// drift from them.

// reservedShorthands are the only single-letter flags, each with one meaning
// across the whole tree.
var reservedShorthands = map[string]string{
	"o": "output",
	"f": "force",
	"q": "quiet",
	"v": "verbose",
	"w": "watch",
	"t": "title",
	"d": "description",
	"c": "category",
	"h": "help",
}

// dataCommands return a result a script may want to read, so each takes
// --json.
var dataCommands = []string{
	"fk env",
	"fk env list",
	"fk auth status",
	"fk org list",
	"fk series list",
	"fk video create",
	"fk video upload",
	"fk video status",
	"fk video view",
	"fk video download",
	"fk archive backfill",
	"fk archive refresh-metadata",
}

func walk(cmd *cobra.Command, visit func(*cobra.Command)) {
	visit(cmd)
	for _, child := range cmd.Commands() {
		walk(child, visit)
	}
}

// allFlags lists a command's own flags and the persistent ones it inherits.
func allFlags(cmd *cobra.Command) []*pflag.Flag {
	var flags []*pflag.Flag
	cmd.Flags().VisitAll(func(f *pflag.Flag) { flags = append(flags, f) })
	cmd.InheritedFlags().VisitAll(func(f *pflag.Flag) { flags = append(flags, f) })
	return flags
}

func TestShortFlagsMeanOneThingEverywhere(t *testing.T) {
	walk(newRootCmd(), func(cmd *cobra.Command) {
		cmd.InitDefaultHelpFlag()
		for _, f := range allFlags(cmd) {
			if f.Shorthand == "" {
				continue
			}
			if want, ok := reservedShorthands[f.Shorthand]; !ok || want != f.Name {
				t.Errorf("%s: -%s is --%s, but -%s is reserved for %q",
					cmd.CommandPath(), f.Shorthand, f.Name, f.Shorthand, want)
			}
		}
	})
}

func TestFlagNamesHaveOneTypeAndDefault(t *testing.T) {
	type shape struct{ typ, def, where string }
	seen := map[string]shape{}
	walk(newRootCmd(), func(cmd *cobra.Command) {
		for _, f := range allFlags(cmd) {
			got := shape{f.Value.Type(), f.DefValue, cmd.CommandPath()}
			if prev, ok := seen[f.Name]; ok && (prev.typ != got.typ || prev.def != got.def) {
				t.Errorf("--%s is %s (default %q) in %s but %s (default %q) in %s",
					f.Name, got.typ, got.def, got.where, prev.typ, prev.def, prev.where)
			}
			seen[f.Name] = got
		}
	})
}

func TestFlagsNameConceptsNotTypes(t *testing.T) {
	walk(newRootCmd(), func(cmd *cobra.Command) {
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Name == "id" || strings.HasSuffix(f.Name, "-id") {
				t.Errorf("%s: --%s should be positional, or named for what it is", cmd.CommandPath(), f.Name)
			}
		})
	})
}

func TestRunnableCommandsDeclareTheirArguments(t *testing.T) {
	walk(newRootCmd(), func(cmd *cobra.Command) {
		if cmd.Runnable() && cmd.Args == nil {
			t.Errorf("%s has no Args rule, so stray arguments would be ignored", cmd.CommandPath())
		}
	})
}

func TestShortHelpIsOneStyle(t *testing.T) {
	walk(newRootCmd(), func(cmd *cobra.Command) {
		short := cmd.Short
		if short == "" {
			t.Errorf("%s has no short help", cmd.CommandPath())
			return
		}
		if first := []rune(short)[0]; !unicode.IsUpper(first) {
			t.Errorf("%s: short help %q should start with a capital", cmd.CommandPath(), short)
		}
		if strings.HasSuffix(short, ".") {
			t.Errorf("%s: short help %q should not end with a period", cmd.CommandPath(), short)
		}
	})
}

func TestDataCommandsTakeJSON(t *testing.T) {
	commands := map[string]*cobra.Command{}
	walk(newRootCmd(), func(cmd *cobra.Command) { commands[cmd.CommandPath()] = cmd })

	for _, path := range dataCommands {
		cmd, ok := commands[path]
		if !ok {
			t.Errorf("%s does not exist", path)
			continue
		}
		if cmd.Flags().Lookup("json") == nil {
			t.Errorf("%s returns data but has no --json", path)
		}
	}
}

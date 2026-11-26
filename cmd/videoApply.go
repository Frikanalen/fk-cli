package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"

	"github/frikanalen/fk-cli/fk-client"

	"github.com/spf13/cobra"
)

// applyResult is how one video appears in "video apply --json".
type applyResult struct {
	Id      int      `json:"id"`
	URL     string   `json:"url"`
	Changed []string `json:"changed"`
	Error   string   `json:"error,omitempty"`
}

func newVideoApplyCmd() *cobra.Command {
	var out output
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "apply [<file>]",
		Short: "Bring videos' metadata in line with JSON or YAML records",
		Long: "Read one or more video records, each naming its video by id, and\n" +
			"change each video's fields to match. The records come from <file>, or\n" +
			"from stdin when no file is given or the file is \"-\". Several records\n" +
			"can be a JSON array, a YAML list or a \"---\"-separated YAML stream.\n" +
			"\n" +
			"Like a patch, only the fields a record names are touched: a record\n" +
			"with just id and description changes just the description. Fields\n" +
			"already holding the given value are not sent, so a whole record from\n" +
			"\"fk video get\" can be applied with one field edited. Read-only fields\n" +
			"are skipped, with a warning when the record tried to change one.\n" +
			"\n" +
			"Prints one line per video saying whether it was configured or\n" +
			"unchanged. Every record is checked before anything is sent; if a\n" +
			"video then fails, the others are still applied and the command exits 1.",
		Example: "  fk video get 12345 --yaml > video.yaml\n" +
			"  $EDITOR video.yaml\n" +
			"  fk video apply video.yaml\n" +
			"\n" +
			"  printf 'id: 12345\\ndescription: |\\n  New text\\n' | fk video apply\n" +
			"  fk video apply descriptions.yaml --dry-run",
		Args: args(0, 1),
		RunE: func(cmd *cobra.Command, argv []string) error {
			data, err := readInput(cmd, argv)
			if err != nil {
				return err
			}
			records, err := applyRecords(data)
			if err != nil {
				return err
			}
			client, err := fk.Open()
			if err != nil {
				return err
			}

			w, errW := cmd.OutOrStdout(), cmd.ErrOrStderr()
			results := []applyResult{}
			failed := false
			for _, rec := range records {
				result := applyResult{Id: rec.id, URL: client.VideoURL(rec.id), Changed: []string{}}
				changed, err := applyOne(cmd.Context(), client, rec, dryRun, errW)
				if err != nil {
					failed = true
					result.Error = err.Error()
					fmt.Fprintf(errW, "fk: video %d: %s\n", rec.id, err)
				} else {
					result.Changed = changed
					if !out.json {
						fmt.Fprintln(w, describeApply(rec.id, changed, dryRun))
					}
				}
				results = append(results, result)
			}
			if out.json {
				if err := printJSON(w, results); err != nil {
					return err
				}
			}
			if failed {
				return errSilentFailure
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would change without changing anything")
	out.addJSONFlag(cmd)
	return cmd
}

// applyRecord is one record of an apply: the video it names and the fields
// it asks for, id and all.
type applyRecord struct {
	id     int
	fields map[string]any
}

// applyRecords parses and checks every record before any is sent, so a typo
// in the tenth record does not leave the first nine applied.
func applyRecords(data []byte) ([]applyRecord, error) {
	docs, err := parseDocuments(data)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, usageErrorf("no video records given")
	}
	records := make([]applyRecord, 0, len(docs))
	seen := map[int]bool{}
	for i, doc := range docs {
		fields, ok := doc.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("record %d is not an object of fields", i+1)
		}
		id, ok := recordID(fields["id"])
		if !ok {
			return nil, fmt.Errorf("record %d has no id saying which video it is for", i+1)
		}
		if seen[id] {
			return nil, fmt.Errorf("video %d appears more than once", id)
		}
		seen[id] = true
		records = append(records, applyRecord{id: id, fields: fields})
	}
	return records, nil
}

func recordID(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, n > 0
	case float64:
		if n > 0 && n == math.Trunc(n) && n < math.MaxInt32 {
			return int(n), true
		}
	}
	return 0, false
}

// applyOne patches the fields of one record that differ from the video as it
// stands, and returns their names.
func applyOne(ctx context.Context, client *fk.Client, rec applyRecord, dryRun bool, warn io.Writer) ([]string, error) {
	current, err := currentFields(ctx, client, rec.id)
	if err != nil {
		return nil, err
	}
	changed, ignored := fieldChanges(current, rec.fields)
	warnIgnored(warn, rec.id, ignored)
	if len(changed) == 0 || dryRun {
		return sortedKeys(changed), nil
	}
	body, err := json.Marshal(changed)
	if err != nil {
		return nil, err
	}
	if _, err := client.PatchVideo(ctx, rec.id, body); err != nil {
		return nil, err
	}
	return sortedKeys(changed), nil
}

func currentFields(ctx context.Context, client *fk.Client, videoId int) (map[string]any, error) {
	raw, err := client.VideoRecord(ctx, videoId)
	if err != nil {
		return nil, err
	}
	var current map[string]any
	if err := json.Unmarshal(raw, &current); err != nil {
		return nil, fmt.Errorf("decoding video %d: %w", videoId, err)
	}
	return current, nil
}

// describeApply is the line apply prints for one video, after kubectl's
// "configured" and "unchanged".
func describeApply(videoId int, changed []string, dryRun bool) string {
	switch {
	case len(changed) == 0:
		return fmt.Sprintf("video %d unchanged", videoId)
	case dryRun:
		return fmt.Sprintf("video %d would change: %s", videoId, strings.Join(changed, ", "))
	default:
		return fmt.Sprintf("video %d configured: %s", videoId, strings.Join(changed, ", "))
	}
}

// editHeader opens the file "video edit" hands to the editor.
const editHeader = "# Edit the fields to change, then save and quit. Only fields whose value\n" +
	"# you change are sent; removing a field leaves it as it is. Read-only\n" +
	"# fields (id, files, organization, series, ...) are ignored; set seriesId\n" +
	"# to move the video to another series. Quit without saving to cancel.\n"

func newVideoEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Edit a video's metadata in your editor",
		Long: "Open a video's record as YAML in $VISUAL or $EDITOR (vi if neither is\n" +
			"set), and when the editor exits, send the fields that were changed.\n" +
			"Saving without changes, or quitting without saving, changes nothing.\n" +
			"\n" +
			"If the server refuses the change, the error names the file holding\n" +
			"your edits, so they can be fixed and sent with \"fk video apply\".",
		Example: "  fk video edit 12345\n" +
			"  EDITOR=nano fk video edit 12345",
		Args: args(1, 1),
		RunE: func(cmd *cobra.Command, argv []string) error {
			videoId, err := parseID(argv[0], "video ID")
			if err != nil {
				return err
			}
			client, err := fk.Open()
			if err != nil {
				return err
			}
			raw, err := client.VideoRecord(cmd.Context(), videoId)
			if err != nil {
				return err
			}
			var current map[string]any
			if err := json.Unmarshal(raw, &current); err != nil {
				return fmt.Errorf("decoding video %d: %w", videoId, err)
			}
			original, err := toYAML(raw)
			if err != nil {
				return err
			}

			file, err := os.CreateTemp("", fmt.Sprintf("fk-video-%d-*.yaml", videoId))
			if err != nil {
				return err
			}
			path := file.Name()
			_, err = file.WriteString(editHeader + string(original))
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				return err
			}
			keep := false
			defer func() {
				if !keep {
					_ = os.Remove(path)
				}
			}()

			if err := runEditor(cmd, path); err != nil {
				return err
			}
			edited, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			desired, err := parseObject(edited)
			if err != nil {
				keep = true
				return withHint(err, "fix %s and run: fk video apply %s", path, path)
			}
			if id, ok := recordID(desired["id"]); ok && id != videoId {
				keep = true
				return fmt.Errorf("the id was changed to %d; edit cancelled, your edits are in %s", id, path)
			}

			changed, ignored := fieldChanges(current, desired)
			warnIgnored(cmd.ErrOrStderr(), videoId, ignored)
			if len(changed) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "fk: video %d: no changes made\n", videoId)
				return nil
			}
			body, err := json.Marshal(changed)
			if err != nil {
				return err
			}
			if _, err := client.PatchVideo(cmd.Context(), videoId, body); err != nil {
				keep = true
				return withHint(err, "fix %s and run: fk video apply %s", path, path)
			}
			fmt.Fprintln(cmd.OutOrStdout(), describeApply(videoId, sortedKeys(changed), false))
			return nil
		},
	}
	return cmd
}

// runEditor opens path in the user's editor on the terminal. The editor
// command goes through the shell, so EDITOR="code --wait" works.
func runEditor(cmd *cobra.Command, path string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	run := exec.CommandContext(cmd.Context(), "sh", "-c", editor+` "$1"`, "sh", path)
	run.Stdin, run.Stdout, run.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := run.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("the editor exited with status %d; nothing was changed", exit.ExitCode())
		}
		return fmt.Errorf("starting the editor %q: %w", editor, err)
	}
	return nil
}

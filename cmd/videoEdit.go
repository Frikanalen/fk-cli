package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github/frikanalen/fk-cli/fk-client"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newVideoGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Print a video's full record as JSON",
		Long: "Print a video's record exactly as the API holds it, every field\n" +
			"included. Unlike \"fk video view --json\", the field names are the API's\n" +
			"own, so the output can be edited and sent back with \"fk video put\".\n" +
			"\n" +
			"Read-only fields (id, files, organization, series and so on) are\n" +
			"ignored when sent back. To move a video to another series, set\n" +
			"seriesId.",
		Example: "  fk video get 12345\n" +
			"  fk video get 12345 | jq .description",
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
			record, err := client.VideoRecord(cmd.Context(), videoId)
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), record)
		},
	}
}

func newVideoPutCmd() *cobra.Command {
	var out output
	cmd := &cobra.Command{
		Use:   "put <id> [<file>]",
		Short: "Replace a video's metadata with a JSON record",
		Long: "Send a whole video record, as \"fk video get\" prints it, back to the\n" +
			"API. The record is read from <file>, or from stdin when no file is\n" +
			"given or the file is \"-\".\n" +
			"\n" +
			"name and categories must be present. Other writable fields left out\n" +
			"keep their current value, and read-only fields are ignored. To change\n" +
			"a few fields without the rest of the record, use \"fk video patch\".\n" +
			"\n" +
			"Prints the video's URL, or the record as it now stands with --json.",
		Example: "  fk video get 12345 > video.json\n" +
			"  $EDITOR video.json\n" +
			"  fk video put 12345 video.json",
		Args: args(1, 2),
		RunE: func(cmd *cobra.Command, argv []string) error {
			videoId, err := parseID(argv[0], "video ID")
			if err != nil {
				return err
			}
			record, err := readRecord(cmd, argv[1:])
			if err != nil {
				return err
			}
			client, err := fk.Open()
			if err != nil {
				return err
			}
			updated, err := client.ReplaceVideo(cmd.Context(), videoId, record)
			if err != nil {
				return err
			}
			return printUpdatedVideo(cmd, client, videoId, updated, out)
		},
	}
	out.addJSONFlag(cmd)
	return cmd
}

// videoPatchOptions holds the flags of "video patch", each a shorthand for
// one field of the JSON body.
type videoPatchOptions struct {
	title       string
	description string
	categories  []string
	seriesId    int
	episode     int
	out         output
}

// apply lays the flags that were given over fields, so a flag wins over the
// same field in a file. A --series or --episode of 0 clears the field.
func (o *videoPatchOptions) apply(fields map[string]any, changed func(string) bool) {
	if changed("title") {
		fields["name"] = o.title
	}
	if changed("description") {
		fields["description"] = o.description
	}
	if changed("category") {
		fields["categories"] = o.categories
	}
	if changed("series") {
		fields["seriesId"] = zeroAsNull(o.seriesId)
	}
	if changed("episode") {
		fields["episodeNumber"] = zeroAsNull(o.episode)
	}
}

func zeroAsNull(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

// anyPatchFlag reports whether any field flag was given.
func anyPatchFlag(changed func(string) bool) bool {
	for _, name := range []string{"title", "description", "category", "series", "episode"} {
		if changed(name) {
			return true
		}
	}
	return false
}

func newVideoPatchCmd() *cobra.Command {
	opts := &videoPatchOptions{}
	cmd := &cobra.Command{
		Use:   "patch <id> [<file>]",
		Short: "Change some of a video's metadata",
		Long: "Change only the fields given, leaving the rest of the video as it is.\n" +
			"Fields come from the flags, from a JSON object in <file>, or both; a\n" +
			"flag wins over the same field in the file. With no flags and no file,\n" +
			"the JSON object is read from stdin, as it is when the file is \"-\".\n" +
			"\n" +
			"Field names are the ones \"fk video get\" prints. --category replaces\n" +
			"the whole list. --series 0 takes the video out of its series, and\n" +
			"--episode 0 clears the episode number.\n" +
			"\n" +
			"Prints the video's URL, or the record as it now stands with --json.",
		Example: "  fk video patch 12345 -d \"A new description\"\n" +
			"  fk video patch 12345 -t \"New title\" -c Kultur -c Debatt\n" +
			"  echo '{\"publishOnWeb\": false}' | fk video patch 12345",
		Args: args(1, 2),
		RunE: func(cmd *cobra.Command, argv []string) error {
			videoId, err := parseID(argv[0], "video ID")
			if err != nil {
				return err
			}
			changed := cmd.Flags().Changed

			fields := map[string]any{}
			if len(argv) == 2 || !anyPatchFlag(changed) {
				raw, err := readRecord(cmd, argv[1:])
				if err != nil {
					return err
				}
				if err := json.Unmarshal(raw, &fields); err != nil {
					return fmt.Errorf("video fields must be a JSON object: %w", err)
				}
			}
			opts.apply(fields, changed)
			if len(fields) == 0 {
				return usageErrorf("nothing to change: give a flag such as -d, or some JSON fields")
			}
			body, err := json.Marshal(fields)
			if err != nil {
				return err
			}

			client, err := fk.Open()
			if err != nil {
				return err
			}
			updated, err := client.PatchVideo(cmd.Context(), videoId, body)
			if err != nil {
				return err
			}
			return printUpdatedVideo(cmd, client, videoId, updated, opts.out)
		},
	}
	flags := cmd.Flags()
	flags.StringVarP(&opts.title, "title", "t", "", "Title of the video")
	flags.StringVarP(&opts.description, "description", "d", "", "Description of the video")
	flags.StringArrayVarP(&opts.categories, "category", "c", nil, "Category name (repeat for several)")
	flags.IntVar(&opts.seriesId, "series", 0, "Series ID to file the video under (see fk series list)")
	flags.IntVar(&opts.episode, "episode", 0, "Episode number within the series")
	opts.out.addJSONFlag(cmd)
	return cmd
}

// readRecord reads a JSON body from the file named in rest, or from stdin
// when rest is empty or names "-". It refuses to wait on a terminal, where a
// forgotten pipe would otherwise look like a hang.
func readRecord(cmd *cobra.Command, rest []string) (json.RawMessage, error) {
	var (
		data []byte
		err  error
	)
	if len(rest) == 1 && rest[0] != "-" {
		if data, err = os.ReadFile(rest[0]); err != nil {
			return nil, err
		}
	} else {
		in := cmd.InOrStdin()
		if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			return nil, usageErrorf("no JSON given: name a file, or pipe the record in on stdin")
		}
		if data, err = io.ReadAll(in); err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("the record is not valid JSON")
	}
	return json.RawMessage(data), nil
}

// printUpdatedVideo reports a successful change: the video's URL, or with
// --json the record the server answered with.
func printUpdatedVideo(cmd *cobra.Command, client *fk.Client, videoId int, record json.RawMessage, out output) error {
	if out.json {
		return printJSON(cmd.OutOrStdout(), record)
	}
	fmt.Fprintln(cmd.OutOrStdout(), client.VideoURL(videoId))
	return nil
}

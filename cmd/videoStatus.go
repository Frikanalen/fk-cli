package cmd

import (
	"fmt"

	"github/frikanalen/fk-cli/fk-client"
	"github/frikanalen/fk-cli/internal/progress"

	"github.com/spf13/cobra"
)

// ingestView is how an ingest job appears in --json output.
type ingestView struct {
	Id int `json:"id"`
	fk.IngestJob
}

func newVideoStatusCmd() *cobra.Command {
	var watch bool
	var out output
	cmd := &cobra.Command{
		Use:   "status <id>",
		Short: "Show how far ingest has got with a video's file",
		Long: "Report the ingest state of a video whose file has been uploaded. With\n" +
			"--watch, follow the job to the end with the progress display \"video\n" +
			"create\" shows.\n" +
			"\n" +
			"Ingest runs on the server whether or not anyone is watching, so this is\n" +
			"how an interrupted display is picked back up. Exits 1 if ingest failed.",
		Example: "  fk video status 12345\n" +
			"  fk video status 12345 --watch",
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

			var job *fk.IngestJob
			if watch {
				bar := progress.New(cmd.ErrOrStderr(), ingestLabel(fk.IngestStatePending), progress.UnitsNone)
				job, err = watchIngest(cmd, client, videoId, bar, fmt.Sprintf("video %d", videoId))
				if job == nil {
					return err
				}
			} else if job, err = client.IngestStatus(cmd.Context(), videoId); err != nil {
				return err
			}

			if out.json {
				if err := printJSON(cmd.OutOrStdout(), ingestView{Id: videoId, IngestJob: *job}); err != nil {
					return err
				}
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), describeIngest(*job))
			}
			if job.State == fk.IngestStateFailed {
				// A failed ingest answers the question but is not a
				// success; a script asking in a loop can tell by the code.
				return errSilentFailure
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "Follow the ingest job until it finishes")
	out.addJSONFlag(cmd)
	return cmd
}

// describeIngest renders a job as one line: the state, plus a percentage
// while it is working or a reason once it has given up.
func describeIngest(job fk.IngestJob) string {
	switch {
	case job.State == fk.IngestStateFailed && job.ErrorCode != "":
		return fmt.Sprintf("%s: %s", job.State, job.ErrorCode)
	case job.PercentageDone != nil:
		return fmt.Sprintf("%s %d%%", job.State, *job.PercentageDone)
	default:
		return job.State
	}
}

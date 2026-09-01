package cmd

import (
	"context"
	"fmt"
	"os"

	"github/frikanalen/fk-cli/fk-client"
	"github/frikanalen/fk-cli/internal/progress"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report how far ingest has got with a video's file",
	Long: "Report the ingest state of a video whose file has already been\n" +
		"uploaded. With --wait, follow the job to completion with the same\n" +
		"progress display \"video create -f\" shows.\n" +
		"\n" +
		"Ingest runs on the server and carries on whether or not anyone is\n" +
		"watching it, so this is how a display interrupted mid-ingest is\n" +
		"picked back up. It does not upload anything; use \"video upload\"\n" +
		"only when the file itself did not make it across.",
	Run: func(cmd *cobra.Command, args []string) {
		client, err := fk.Open()
		checkErr(err)

		videoId, err := cmd.Flags().GetInt("id")
		checkErr(err)

		wait, err := cmd.Flags().GetBool("wait")
		checkErr(err)

		ctx := context.Background()

		if wait {
			bar := progress.New(os.Stderr, ingestLabel(fk.IngestStatePending), progress.UnitsNone)
			if err := watchIngest(ctx, client, videoId, bar, fmt.Sprintf("video %d", videoId)); err != nil {
				fatalWithHint(err)
			}
			return
		}

		job, err := client.IngestStatus(ctx, videoId)
		checkErr(err)
		fmt.Println(describeIngest(*job))
		if job.State == fk.IngestStateFailed {
			// A failed ingest is the answer to the question, but it is not
			// a success, and a script asking in a loop should be able to
			// tell without parsing the line.
			os.Exit(1)
		}
	},
}

// describeIngest renders a job as one line for a single status check: the
// state, plus whatever else it has to add -- a percentage while it is
// working, a reason once it has given up.
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

func init() {
	videoCmd.AddCommand(statusCmd)
	statusCmd.Flags().IntP("id", "i", 0, "ID of the video to report on")
	_ = statusCmd.MarkFlagRequired("id")
	statusCmd.Flags().BoolP("wait", "w", false, "Follow the ingest job until it finishes, reporting progress")
}

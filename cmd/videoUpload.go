package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github/frikanalen/fk-cli/fk-client"
	"github/frikanalen/fk-cli/internal/progress"

	"github.com/spf13/cobra"
)

// uploadAndWait uploads fileSpec for videoId and, when wait is set, follows
// the resulting ingest job until it finishes. It is shared by "video upload"
// and "video create". Both phases share one progress bar on stderr: animated
// on a terminal, periodic plain lines otherwise.
func uploadAndWait(cmd *cobra.Command, client *fk.Client, videoId int, fileSpec string, wait bool) error {
	name := filepath.Base(fileSpec)

	// Upload and ingest are two phases of one operation, so they take turns
	// on a single bar rather than leaving a trail of finished ones behind.
	bar := progress.New(cmd.ErrOrStderr(), "Uploading "+name, progress.UnitsBytes)
	err := client.UploadWithProgress(cmd.Context(), videoId, fileSpec, func(uploaded, total int64) {
		bar.Update(uploaded, total)
	})
	if err != nil {
		bar.Fail("upload failed")
		// Nothing usable reached the server, so the file has to go again.
		return withHint(err, "fk video upload %d %s", videoId, shellQuote(fileSpec))
	}

	if !wait {
		bar.Finish("uploaded")
		return nil
	}

	bar.Next(ingestLabel(fk.IngestStatePending), progress.UnitsNone)
	_, err = watchIngest(cmd, client, videoId, bar, name)
	return err
}

// watchIngest follows videoId's ingest job on an already-running bar, which it
// finishes one way or the other. closingLabel names what the closing line is
// about: the file as a whole, rather than whichever ingest stage ran last.
//
// It is shared by the tail of an upload and by "video status --watch", which is
// the same watch started on its own: the job belongs to the server, so a watch
// that was interrupted can simply be opened again.
func watchIngest(cmd *cobra.Command, client *fk.Client, videoId int, bar *progress.Bar, closingLabel string) (*fk.IngestJob, error) {
	job, err := client.WaitForIngest(cmd.Context(), videoId, fk.IngestWatch{
		OnUpdate: func(job fk.IngestJob) {
			bar.SetLabel(ingestLabel(job.State))
			if job.PercentageDone != nil {
				bar.Update(int64(*job.PercentageDone), 100)
			} else {
				// No percentage for this stage: let the bar sweep instead.
				bar.Update(0, -1)
			}
		},
		OnRetry: func(err error, failingFor time.Duration) {
			// The job is still running on the server; it is only our view
			// of it that has gone, so say that rather than failing the bar.
			bar.SetLabel(ingestLabel(labelReconnecting))
			bar.Update(0, -1)
		},
	})
	if err != nil {
		bar.Fail("ingest interrupted")
		// The file is on the server and ingest carries on without us; all
		// that was lost is the watching, so that is all to start again.
		return nil, withHint(err, "fk video status %d --watch", videoId)
	}
	if job.State == fk.IngestStateFailed {
		bar.Fail("ingest failed")
		return job, fmt.Errorf("ingest failed: %s", job.ErrorCode)
	}
	bar.SetLabel(closingLabel)
	bar.Finish("ready to broadcast")
	return job, nil
}

// shellQuote quotes s for a POSIX shell when it needs it, so a hint can be
// pasted back as it stands.
func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || r == '+' || r == ':' || r == ',' || r == '@' ||
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// labelReconnecting stands in for an ingest state while we cannot read one.
const labelReconnecting = "reconnecting"

// ingestLabel names the current ingest stage, padded to the width of the
// longest one so the bar beside it does not jump around as stages change.
func ingestLabel(state string) string {
	return fmt.Sprintf("%-*s", len(labelReconnecting), state)
}

func newVideoUploadCmd() *cobra.Command {
	var noWait bool
	var out output
	cmd := &cobra.Command{
		Use:   "upload <id> <file>",
		Short: "Upload a file for an existing video",
		Long: "Upload a file for a video that already exists, e.g. to retry a failed\n" +
			"upload, and wait for ingest to finish. The video's URL is printed on\n" +
			"stdout. To create a video and upload in one go, use \"fk video create\".",
		Example: "  fk video upload 12345 talk.mov",
		Args:    args(2, 2),
		RunE: func(cmd *cobra.Command, argv []string) error {
			videoId, err := parseID(argv[0], "video ID")
			if err != nil {
				return err
			}
			client, err := fk.Open()
			if err != nil {
				return err
			}
			if err := uploadAndWait(cmd, client, videoId, argv[1], !noWait); err != nil {
				return err
			}
			return printVideoResult(cmd, client, videoId, out)
		},
	}
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "Return once the file is uploaded, without waiting for ingest")
	out.addJSONFlag(cmd)
	return cmd
}

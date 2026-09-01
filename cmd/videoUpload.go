package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github/frikanalen/fk-cli/fk-client"
	"github/frikanalen/fk-cli/internal/progress"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// uploadAndWait uploads fileSpec for videoId and, unless wait is false, follows
// the resulting ingest job until it finishes, then prints the video's URL. It
// is shared by "video upload" and by "video create -f". Both phases share one
// animated progress bar when stderr is a terminal, and become periodic plain
// lines when it is not.
func uploadAndWait(ctx context.Context, client *fk.Client, videoId int, fileSpec string, wait bool) error {
	name := filepath.Base(fileSpec)

	// Upload and ingest are two phases of one operation, so they take turns
	// on a single bar rather than leaving a trail of finished ones behind.
	bar := progress.New(os.Stderr, "Uploading "+name, progress.UnitsBytes)
	err := client.UploadWithProgress(ctx, videoId, fileSpec, func(uploaded, total int64) {
		bar.Update(uploaded, total)
	})
	if err != nil {
		bar.Fail("upload failed")
		// Nothing usable reached the server, so the file has to go again.
		return retryHint{err, fmt.Sprintf("fk video upload -i %d -f %s", videoId, fileSpec)}
	}

	if !wait {
		bar.Finish("uploaded")
		printVideoURL(client, videoId)
		return nil
	}

	bar.Next(ingestLabel(fk.IngestStatePending), progress.UnitsNone)
	return watchIngest(ctx, client, videoId, bar, name)
}

// watchIngest follows videoId's ingest job on an already-running bar, which it
// finishes one way or the other, and prints the video's URL once the job is
// done. closingLabel names what the closing line is about -- the file as a
// whole, rather than whichever ingest stage happened to run last.
//
// It is shared by the tail of an upload and by "video status --wait", which is
// the same watch started on its own: the job belongs to the server, so a watch
// that was interrupted can simply be opened again.
func watchIngest(ctx context.Context, client *fk.Client, videoId int, bar *progress.Bar, closingLabel string) error {
	job, err := client.WaitForIngest(ctx, videoId, fk.IngestWatch{
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
		return retryHint{err, fmt.Sprintf("fk video status -i %d --wait", videoId)}
	}
	if job.State == fk.IngestStateFailed {
		bar.Fail("ingest failed")
		return fmt.Errorf("ingest failed: %s", job.ErrorCode)
	}
	bar.SetLabel(closingLabel)
	bar.Finish("ready to broadcast")

	printVideoURL(client, videoId)
	return nil
}

// printVideoURL writes the video's page address to stdout, keeping it apart
// from the progress bar on stderr so it stays pipeable.
func printVideoURL(client *fk.Client, videoId int) {
	fmt.Println(client.VideoURL(videoId))
}

// labelReconnecting stands in for an ingest state while we cannot read one.
const labelReconnecting = "reconnecting"

// ingestLabel names the current ingest stage, padded to the width of the
// longest one so the bar beside it does not jump around as stages change.
func ingestLabel(state string) string {
	return fmt.Sprintf("%-*s", len(labelReconnecting), state)
}

// retryHint pairs a failure with the command that picks up where it left off.
// Which command that is depends on what was actually lost: an upload that did
// not land has to send the file again, while an interrupted status display
// only has to be reopened.
type retryHint struct {
	err error
	cmd string
}

func (h retryHint) Error() string { return h.err.Error() }
func (h retryHint) Unwrap() error { return h.err }

// fatalWithHint reports err and exits, naming the command to pick things back
// up with when the failure came with one.
func fatalWithHint(err error) {
	var hint retryHint
	if errors.As(err, &hint) {
		log.Errorf("retry with: %s", hint.cmd)
	}
	log.Fatal(err)
}

var uploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "Upload a file for an existing video",
	Long: "Upload a file for a video that already exists, e.g. to retry a failed\n" +
		"upload. To create a video and upload in one go, use \"video create -f\".",
	Run: func(cmd *cobra.Command, args []string) {
		client, err := fk.Open()
		if err != nil {
			log.Fatal(err)
		}

		videoId, err := cmd.Flags().GetInt("id")
		if err != nil {
			log.Fatal(err)
		}

		fileSpec, err := cmd.Flags().GetString("file")
		if err != nil {
			log.Fatal(err)
		}

		wait, err := cmd.Flags().GetBool("wait")
		if err != nil {
			log.Fatal(err)
		}

		if err := uploadAndWait(context.Background(), client, videoId, fileSpec, wait); err != nil {
			fatalWithHint(err)
		}
	},
}

func init() {
	videoCmd.AddCommand(uploadCmd)
	uploadCmd.Flags().IntP("id", "i", 0, "ID of the video to upload a file for")
	_ = uploadCmd.MarkFlagRequired("id")
	uploadCmd.Flags().StringP("file", "f", "", "Path to file to upload")
	_ = uploadCmd.MarkFlagRequired("file")
	uploadCmd.Flags().Bool("wait", true, "Wait for ingest to finish and report its progress")
}

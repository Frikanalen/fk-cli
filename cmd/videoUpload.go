package cmd

import (
	"context"
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
		return err
	}

	if !wait {
		bar.Finish("uploaded")
		printVideoURL(client, videoId)
		return nil
	}

	bar.Next(ingestLabel(fk.IngestStatePending), progress.UnitsNone)
	job, err := client.WaitForIngest(ctx, videoId, 30*time.Minute, func(job fk.IngestJob) {
		bar.SetLabel(ingestLabel(job.State))
		if job.PercentageDone != nil {
			bar.Update(int64(*job.PercentageDone), 100)
		} else {
			// No percentage for this stage: let the bar sweep instead.
			bar.Update(0, -1)
		}
	})
	if err != nil {
		bar.Fail("ingest interrupted")
		return err
	}
	if job.State == fk.IngestStateFailed {
		bar.Fail("ingest failed")
		return fmt.Errorf("ingest failed: %s", job.ErrorCode)
	}
	// The closing line is about the file as a whole, not the ingest stage
	// that happened to run last.
	bar.SetLabel(name)
	bar.Finish("ready to broadcast")

	printVideoURL(client, videoId)
	return nil
}

// printVideoURL writes the video's page address to stdout, keeping it apart
// from the progress bar on stderr so it stays pipeable.
func printVideoURL(client *fk.Client, videoId int) {
	fmt.Println(client.VideoURL(videoId))
}

// ingestLabel names the current ingest stage, padded to the width of the
// longest one so the bar beside it does not jump around as stages change.
func ingestLabel(state string) string {
	return fmt.Sprintf("%-11s", state)
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
			log.Fatal(err)
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

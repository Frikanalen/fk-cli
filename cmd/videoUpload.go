package cmd

import (
	"context"
	"fmt"
	"time"

	"github/frikanalen/fk-cli/fk-client"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// uploadAndWait uploads fileSpec for videoId and, unless wait is false, follows
// the resulting ingest job until it finishes. It is shared by "video upload"
// and by "video create -f".
func uploadAndWait(ctx context.Context, client *fk.Client, videoId int, fileSpec string, wait bool) error {
	log.Infoln("Uploading", fileSpec, "for video", videoId)
	if err := client.Upload(ctx, videoId, fileSpec); err != nil {
		return err
	}
	log.Infoln("Upload complete")

	if !wait {
		return nil
	}

	job, err := client.WaitForIngest(ctx, videoId, 30*time.Minute, func(job fk.IngestJob) {
		if job.PercentageDone != nil {
			log.Infof("ingest: %s (%d%%)", job.State, *job.PercentageDone)
		} else {
			log.Infoln("ingest:", job.State)
		}
	})
	if err != nil {
		return err
	}
	if job.State == fk.IngestStateFailed {
		return fmt.Errorf("ingest failed: %s", job.ErrorCode)
	}
	return nil
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

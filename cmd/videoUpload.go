package cmd

import (
	"context"
	"time"

	"github/frikanalen/fk-cli/fk-client"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var uploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "Upload a file for an existing video",
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

		ctx := context.Background()

		log.Infoln("Uploading", fileSpec, "for video", videoId)
		if err := client.Upload(ctx, videoId, fileSpec); err != nil {
			log.Fatal(err)
		}
		log.Infoln("Upload complete")

		if !wait {
			return
		}

		job, err := client.WaitForIngest(ctx, videoId, 30*time.Minute, func(job fk.IngestJob) {
			if job.PercentageDone != nil {
				log.Infof("ingest: %s (%d%%)", job.State, *job.PercentageDone)
			} else {
				log.Infoln("ingest:", job.State)
			}
		})
		if err != nil {
			log.Fatal(err)
		}
		if job.State == fk.IngestStateFailed {
			log.Fatalln("ingest failed:", job.ErrorCode)
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

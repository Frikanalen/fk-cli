package cmd

import (
	"context"
	"fmt"

	"github/frikanalen/fk-cli/fk-client"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newVideoFromFlags(flags *pflag.FlagSet) (*fk.CreateVideoRequest, error) {
	title, err := flags.GetString("title")
	if err != nil {
		return nil, err
	}

	description, err := flags.GetString("description")
	if err != nil {
		return nil, err
	}

	categories, err := flags.GetStringSlice("category")
	if err != nil {
		return nil, err
	}

	req := &fk.CreateVideoRequest{
		Title:       title,
		Description: description,
		Categories:  categories,
	}

	if flags.Changed("org-id") {
		orgId, err := flags.GetInt("org-id")
		if err != nil {
			return nil, err
		}
		req.OrgId = &orgId
	}

	if flags.Changed("series-id") {
		seriesId, err := flags.GetInt("series-id")
		if err != nil {
			return nil, err
		}
		req.SeriesId = &seriesId
	}

	return req, nil
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create video, optionally uploading a file for it",
	Long: "Create a video. With -f, the file is uploaded for the newly created\n" +
		"video as well, and the video's URL is printed once it is ready. If\n" +
		"anything fails, the command reports the ID and the command to carry\n" +
		"on with: \"video upload\" when the file did not make it across, and\n" +
		"\"video status --wait\" when it did and only the watching stopped.",
	Run: func(cmd *cobra.Command, args []string) {
		client, err := fk.Open()
		if err != nil {
			log.Fatal(err)
		}

		newVideoBody, err := newVideoFromFlags(cmd.Flags())
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

		videoId, err := client.CreateVideo(ctx, *newVideoBody)
		if err != nil {
			log.Fatal(err)
		}

		if fileSpec == "" {
			fmt.Println(client.VideoURL(videoId))
			return
		}

		// The ID is not printed up front any more, so the failure has to hand
		// it over -- as part of the command that resumes from wherever it got
		// to, which is not the same command for a lost upload as for a lost
		// view of the ingest that follows one.
		if err := uploadAndWait(ctx, client, videoId, fileSpec, wait); err != nil {
			fatalWithHint(err)
		}
	},
}

func init() {
	videoCmd.AddCommand(createCmd)
	createCmd.Flags().StringP("title", "t", "", "Title of video")
	_ = createCmd.MarkFlagRequired("title")
	createCmd.Flags().StringP("description", "d", "", "Description of video")
	createCmd.Flags().StringSliceP("category", "c", []string{}, "Category name (repeatable)")
	_ = createCmd.MarkFlagRequired("category")
	createCmd.Flags().IntP("org-id", "o", 0, "Organization ID (only needed if you edit more than one)")
	createCmd.Flags().IntP("series-id", "s", 0, "Series ID to file the video under (see \"fk series list\")")
	createCmd.Flags().StringP("file", "f", "", "Path to file to upload for the new video")
	createCmd.Flags().Bool("wait", true, "With -f, wait for ingest to finish and report its progress")
}

package cmd

import (
	"fmt"
	"strings"

	"github/frikanalen/fk-cli/fk-client"

	"github.com/spf13/cobra"
)

func newVideoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "video",
		Short: "Work with videos",
	}
	cmd.AddCommand(
		newVideoCreateCmd(),
		newVideoUploadCmd(),
		newVideoStatusCmd(),
		newVideoViewCmd(),
		newVideoDownloadCmd(),
	)
	return cmd
}

func newVideoViewCmd() *cobra.Command {
	var out output
	cmd := &cobra.Command{
		Use:   "view <id>",
		Short: "Show a video",
		Args:  args(1, 1),
		RunE: func(cmd *cobra.Command, argv []string) error {
			videoId, err := parseID(argv[0], "video ID")
			if err != nil {
				return err
			}
			client, err := fk.Open()
			if err != nil {
				return err
			}
			video, err := client.Video(cmd.Context(), videoId)
			if err != nil {
				return err
			}
			if out.json {
				return printJSON(cmd.OutOrStdout(), video)
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "id:          %d\n", video.Id)
			fmt.Fprintf(w, "title:       %s\n", video.Title)
			if video.Org != nil {
				fmt.Fprintf(w, "org:         %d (%s)\n", video.Org.Id, video.Org.Name)
			}
			if video.Series != nil {
				fmt.Fprintf(w, "series:      %d (%s)\n", video.Series.Id, video.Series.Name)
			}
			fmt.Fprintf(w, "categories:  %s\n", strings.Join(video.Categories, ", "))
			fmt.Fprintf(w, "duration:    %s\n", video.Duration)
			fmt.Fprintf(w, "variants:    %s\n", strings.Join(video.Variants, ", "))
			fmt.Fprintf(w, "url:         %s\n", video.URL)
			if video.Description != "" {
				fmt.Fprintf(w, "\n%s\n", video.Description)
			}
			return nil
		},
	}
	out.addJSONFlag(cmd)
	return cmd
}

// videoResult is what create and upload print with --json.
type videoResult struct {
	Id  int    `json:"id"`
	URL string `json:"url"`
}

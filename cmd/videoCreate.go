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

	return req, nil
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create video",
	Run: func(cmd *cobra.Command, args []string) {
		client, err := fk.Open()
		if err != nil {
			log.Fatal(err)
		}

		newVideoBody, err := newVideoFromFlags(cmd.Flags())
		if err != nil {
			log.Fatal(err)
		}

		videoId, err := client.CreateVideo(context.Background(), *newVideoBody)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println(videoId)
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
}

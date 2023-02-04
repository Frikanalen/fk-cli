package cmd

import (
	"context"
	"fmt"
	"github/frikanalen/fk-cli/fk-client"
	"os"

	log "github.com/sirupsen/logrus"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newVideoFromFlags(flags *pflag.FlagSet) (*fk.NewVideoJSONRequestBody, error) {

	mediaId, err := flags.GetInt("mediaId")
	if err != nil {
		return nil, err
	}

	categoryIds, err := flags.GetIntSlice("categoryIds")
	if err != nil {
		return nil, err
	}

	title, err := flags.GetString("title")
	if err != nil {
		return nil, err
	}

	desc := new(string)
	*desc, _ = flags.GetString("description")

	return &fk.NewVideoJSONRequestBody{
		Title:       title,
		Description: desc,
		MediaId:     mediaId,
		Categories:  &categoryIds,
	}, nil
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create video",
	Run: func(cmd *cobra.Command, args []string) {
		orgId, err := cmd.Flags().GetInt("orgId")
		if err != nil {
			log.Fatal(err)
			os.Exit(1)
		}

		client, err := fk.Open()
		if err != nil {
			log.Fatal(err)
			os.Exit(1)
		}

		newVideoBody, err := newVideoFromFlags(cmd.Flags())
		if err != nil {
			log.Fatal(err)
			os.Exit(1)
		}

		newVideoResponse, err := client.NewVideo(context.Background(), orgId, *newVideoBody)
		if err != nil {
			log.Fatal(err)
			os.Exit(1)
		}

		newVideo, err := fk.ParseNewVideoResponse(newVideoResponse)
		if err != nil {
			log.Fatal(err)
			os.Exit(1)
		}

		fmt.Println(*newVideo.JSON201.Id)
	},
}

func init() {
	videoCmd.AddCommand(createCmd)
	createCmd.Flags().StringP("title", "t", "", "Title of video")
	_ = createCmd.MarkFlagRequired("title")
	createCmd.Flags().StringP("description", "d", "", "Description of video")
	createCmd.Flags().IntP("orgId", "o", 0, "Organization ID")
	_ = createCmd.MarkFlagRequired("orgId")
	createCmd.Flags().IntSliceP("categoryIds", "c", []int{}, "List of categories")
	_ = createCmd.MarkFlagRequired("categoryIds")
	createCmd.Flags().IntP("mediaId", "m", 0, "Media ID")
	_ = createCmd.MarkFlagRequired("mediaId")
	createCmd.Flags().BoolP("jukeboxable", "j", false, "allow automatic scheduling")
}

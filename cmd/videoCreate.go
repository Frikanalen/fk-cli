package cmd

import (
	"fmt"

	"github/frikanalen/fk-cli/fk-client"

	"github.com/spf13/cobra"
)

// videoCreateOptions holds the flags of "video create".
type videoCreateOptions struct {
	title       string
	description string
	categories  []string
	orgId       int
	seriesId    int
	noWait      bool
	out         output
}

// request builds the API request from the options. Changed reports which
// optional flags were given, so an unset --org or --series is left out
// rather than sent as 0.
func (o *videoCreateOptions) request(changed func(string) bool) fk.CreateVideoRequest {
	req := fk.CreateVideoRequest{
		Title:       o.title,
		Description: o.description,
		Categories:  o.categories,
	}
	if changed("org") {
		orgId := o.orgId
		req.OrgId = &orgId
	}
	if changed("series") {
		seriesId := o.seriesId
		req.SeriesId = &seriesId
	}
	return req
}

func newVideoCreateCmd() *cobra.Command {
	opts := &videoCreateOptions{}
	cmd := &cobra.Command{
		Use:   "create [<file>]",
		Short: "Create a video, and upload its file if one is given",
		Long: "Create a video. Given a file, upload it and wait for ingest to finish,\n" +
			"showing progress on stderr. The video's URL is printed on stdout.\n" +
			"\n" +
			"If anything fails after the video exists, the error names the command\n" +
			"to carry on with: \"fk video upload\" when the file did not make it\n" +
			"across, \"fk video status --watch\" when only the watching stopped.",
		Example: "  fk video create talk.mov -t \"Opening talk\" -c Kultur\n" +
			"  fk video create -t \"Placeholder\" -c Annet --series 12",
		Args: args(0, 1),
		RunE: func(cmd *cobra.Command, argv []string) error {
			client, err := fk.Open()
			if err != nil {
				return err
			}
			videoId, err := client.CreateVideo(cmd.Context(), opts.request(cmd.Flags().Changed))
			if err != nil {
				return err
			}

			if len(argv) == 1 {
				// The ID is not printed up front, so a failure hands it over
				// as part of the command that resumes from where it stopped.
				if err := uploadAndWait(cmd, client, videoId, argv[0], !opts.noWait); err != nil {
					return err
				}
			}
			return printVideoResult(cmd, client, videoId, opts.out)
		},
	}
	flags := cmd.Flags()
	flags.StringVarP(&opts.title, "title", "t", "", "Title of the video")
	flags.StringVarP(&opts.description, "description", "d", "", "Description of the video")
	flags.StringArrayVarP(&opts.categories, "category", "c", nil, "Category name (repeat for several)")
	flags.IntVar(&opts.orgId, "org", 0, "Organization ID (only needed if you belong to several; see fk org list)")
	flags.IntVar(&opts.seriesId, "series", 0, "Series ID to file the video under (see fk series list)")
	flags.BoolVar(&opts.noWait, "no-wait", false, "Return once the file is uploaded, without waiting for ingest")
	_ = cmd.MarkFlagRequired("title")
	_ = cmd.MarkFlagRequired("category")
	opts.out.addJSONFlag(cmd)
	return cmd
}

// printVideoResult writes the video's URL, or its ID and URL as JSON, to
// stdout, apart from the progress shown on stderr so it stays pipeable.
func printVideoResult(cmd *cobra.Command, client *fk.Client, videoId int, out output) error {
	result := videoResult{Id: videoId, URL: client.VideoURL(videoId)}
	if out.json {
		return printJSON(cmd.OutOrStdout(), result)
	}
	fmt.Fprintln(cmd.OutOrStdout(), result.URL)
	return nil
}

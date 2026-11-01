package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"

	"github/frikanalen/fk-cli/fk-client"
	"github/frikanalen/fk-cli/internal/progress"

	"github.com/spf13/cobra"
)

// downloadResult is what "video download --json" prints.
type downloadResult struct {
	Id      int    `json:"id"`
	Variant string `json:"variant"`
	Path    string `json:"path"`
}

func newVideoDownloadCmd() *cobra.Command {
	var variant, outputPath string
	var force bool
	var out output
	cmd := &cobra.Command{
		Use:   "download <id>",
		Short: "Download one of a video's files",
		Long: "Download a file registered for a video. By default this is the\n" +
			"original upload; --variant picks another, e.g. broadcast or webm_med\n" +
			"(\"fk video view\" lists them).\n" +
			"\n" +
			"The file lands in the current directory under its archive name unless\n" +
			"--output says otherwise, and an existing file is left alone unless\n" +
			"--force is given. The saved path is printed on stdout.",
		Example: "  fk video download 12345\n" +
			"  fk video download 12345 --variant broadcast -o ~/Downloads/",
		Args: args(1, 1),
		RunE: func(cmd *cobra.Command, argv []string) error {
			videoId, err := parseID(argv[0], "video ID")
			if err != nil {
				return err
			}
			client, err := fk.Open()
			if err != nil {
				return err
			}
			dest, err := downloadVideoFile(cmd, client, videoId, variant, outputPath, force)
			if err != nil {
				return err
			}
			if out.json {
				return printJSON(cmd.OutOrStdout(), downloadResult{Id: videoId, Variant: variant, Path: dest})
			}
			fmt.Fprintln(cmd.OutOrStdout(), dest)
			return nil
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&variant, "variant", fk.VariantOriginal, "Which of the video's files to fetch")
	flags.StringVarP(&outputPath, "output", "o", "", "File path or existing directory to save to (default: archive file name in the current directory)")
	flags.BoolVarP(&force, "force", "f", false, "Overwrite the destination if it already exists")
	out.addJSONFlag(cmd)
	return cmd
}

func downloadVideoFile(cmd *cobra.Command, client *fk.Client, videoId int, variant, output string, force bool) (string, error) {
	link, err := client.VideoFile(cmd.Context(), videoId, variant)
	if err != nil {
		return "", err
	}

	dest, err := downloadDest(link.URL, output)
	if err != nil {
		return "", err
	}
	if !force {
		if _, err := os.Stat(dest); err == nil {
			return "", fmt.Errorf("%s already exists; pass --force to overwrite it", dest)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}

	bar := progress.New(cmd.ErrOrStderr(), "Downloading "+filepath.Base(dest), progress.UnitsBytes)
	err = client.Download(cmd.Context(), link.URL, dest, func(received, total int64) {
		bar.Update(received, total)
	})
	if err != nil {
		bar.Fail("download failed")
		return "", err
	}
	bar.Finish("downloaded")
	return dest, nil
}

// downloadDest decides where a download goes: the --output path as given,
// inside it when it names a directory, or the archive's own file name in the
// current directory when it is empty.
func downloadDest(fileURL, output string) (string, error) {
	u, err := url.Parse(fileURL)
	if err != nil {
		return "", err
	}
	name := path.Base(u.Path)
	if name == "/" || name == "." {
		return "", fmt.Errorf("cannot tell a file name from %s; pass --output", fileURL)
	}

	if output == "" {
		return name, nil
	}
	if info, err := os.Stat(output); err == nil && info.IsDir() {
		return filepath.Join(output, name), nil
	}
	return output, nil
}

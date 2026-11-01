package cmd

import (
	"context"
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

var downloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Download one of a video's files",
	Long: "Download a file registered for a video. By default this is the\n" +
		"original upload; --variant picks another, e.g. broadcast or webm_med.\n" +
		"\n" +
		"The file lands in the current directory under its archive name unless\n" +
		"--output says otherwise, and an existing file is left alone unless\n" +
		"--force is given. The saved path is printed on stdout.",
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		client, err := fk.Open()
		checkErr(err)

		videoId, err := cmd.Flags().GetInt("id")
		checkErr(err)
		variant, err := cmd.Flags().GetString("variant")
		checkErr(err)
		output, err := cmd.Flags().GetString("output")
		checkErr(err)
		force, err := cmd.Flags().GetBool("force")
		checkErr(err)

		dest, err := downloadVideoFile(context.Background(), client, videoId, variant, output, force)
		checkErr(err)
		fmt.Println(dest)
	},
}

func downloadVideoFile(ctx context.Context, client *fk.Client, videoId int, variant, output string, force bool) (string, error) {
	link, err := client.VideoFile(ctx, videoId, variant)
	if err != nil {
		return "", err
	}

	dest, err := downloadDest(link.URL, output)
	if err != nil {
		return "", err
	}
	if !force {
		if _, err := os.Stat(dest); err == nil {
			return "", fmt.Errorf("%s already exists; use --force to overwrite it", dest)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}

	bar := progress.New(os.Stderr, "Downloading "+filepath.Base(dest), progress.UnitsBytes)
	err = client.Download(ctx, link.URL, dest, func(received, total int64) {
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

func init() {
	videoCmd.AddCommand(downloadCmd)
	downloadCmd.Flags().IntP("id", "i", 0, "ID of the video to download a file of")
	_ = downloadCmd.MarkFlagRequired("id")
	downloadCmd.Flags().StringP("variant", "V", fk.VariantOriginal, "Which of the video's files to fetch (original, broadcast, webm_med, ...)")
	downloadCmd.Flags().StringP("output", "o", "", "Where to save it: a file path or an existing directory (default: archive file name in the current directory)")
	downloadCmd.Flags().BoolP("force", "F", false, "Overwrite the destination if it already exists")
}

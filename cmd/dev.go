package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

func newDevCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Tools for developing and testing Frikanalen",
	}
	cmd.AddCommand(newDevTestcardCmd())
	return cmd
}

// testcardOptions holds the flags of "dev testcard".
type testcardOptions struct {
	output   string
	duration time.Duration
	text     string
	font     string
}

func newDevTestcardCmd() *cobra.Command {
	opts := &testcardOptions{}
	cmd := &cobra.Command{
		Use:   "testcard",
		Short: "Generate a test video",
		Long: "Generate a 1280x720 50p test pattern with a caption and a running\n" +
			"timecode, for exercising upload and playout. Needs ffmpeg on $PATH.\n" +
			"The output path is printed on stdout.",
		Example: "  fk dev testcard -o test.mov\n" +
			"  fk dev testcard -o long.mov --duration 2m --text \"Soak test\"",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.duration <= 0 {
				return usageErrorf("--duration must be positive, got %s", opts.duration)
			}
			if err := makeTestcard(cmd, opts); err != nil {
				return fmt.Errorf("generating test video: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), opts.output)
			return nil
		},
	}
	flags := cmd.Flags()
	flags.StringVarP(&opts.output, "output", "o", "", "File to write; the extension picks the container")
	flags.DurationVar(&opts.duration, "duration", 10*time.Second, "Length of the video, e.g. 30s or 2m")
	flags.StringVar(&opts.text, "text", "test video", "Caption to superimpose")
	flags.StringVar(&opts.font, "font", "", "Font file for the caption (default: fontconfig's monospace)")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func makeTestcard(cmd *cobra.Command, opts *testcardOptions) error {
	textoptions := ffmpeg.KwArgs{
		"box":          "true",
		"fontsize":     72,
		"boxborderw":   20,
		"fontcolor":    "white",
		"boxcolor":     "black",
		"line_spacing": "20",
		"x":            "(w-text_w)/2",
		"y":            "(h-text_h-line_h)/2",
		"expansion":    "normal",
	}
	if opts.font != "" {
		textoptions["fontfile"] = opts.font
	} else {
		textoptions["font"] = "monospace"
	}

	text := strings.ReplaceAll(opts.text, ":", `\:`)
	text = strings.ReplaceAll(text, "'", `\'`)
	text = strings.ReplaceAll(text, "\n", `\n`) + "\n"

	return ffmpeg.
		Input(fmt.Sprintf("testsrc=duration=%f:size=1280x720:rate=50", opts.duration.Seconds()), ffmpeg.KwArgs{"f": "lavfi"}).
		Drawtext(text+`%{pts\:hms}`, 0, 0, false, textoptions).
		Output(opts.output, ffmpeg.KwArgs{"pix_fmt": "yuv422p"}).
		OverWriteOutput().
		WithErrorOutput(cmd.ErrOrStderr()).
		Run()
}

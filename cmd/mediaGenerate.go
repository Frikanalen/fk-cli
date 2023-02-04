/*
Copyright © 2021 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

func makeTestvideo(duration float64, text string, filepath string) error {
	textoptions := ffmpeg.KwArgs{
		"box":          "true",
		"fontsize":     72,
		"boxborderw":   20,
		"fontfile":     getFont(),
		"fontcolor":    "white",
		"boxcolor":     "black",
		"line_spacing": "20",
		"x":            "(w-text_w)/2",
		"y":            "(h-text_h-line_h)/2",
		"expansion":    "normal",
	}

	text = strings.ReplaceAll(text, ":", `\:`)
	text = strings.ReplaceAll(text, "'", `\'`)
	text = strings.ReplaceAll(text, "\n", `\n`) + "\n"

	err := ffmpeg.
		Input(fmt.Sprintf("testsrc=duration=%f:size=1280x720:rate=50", duration), ffmpeg.KwArgs{"f": "lavfi"}).
		Drawtext(text+`%{pts\:hms}`, 0, 0, false, textoptions).
		Output(filepath, ffmpeg.KwArgs{"pix_fmt": "yuv422p"}).
		OverWriteOutput().ErrorToStdOut().
		Run()

	return err
}

func getFont() string {
	return "/usr/share/fonts/truetype/inconsolata/Inconsolata.otf"
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "generate test video",

	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("generate called")
		duration, _ := cmd.Flags().GetFloat64("seconds")
		err := makeTestvideo(
			duration,
			cmd.Flag("text").Value.String(),
			cmd.Flag("output").Value.String(),
		)
		if err != nil {
			log.Errorln("generete test media failed, %w", err)
		}
	},
}

func init() {
	mediaCmd.AddCommand(generateCmd)
	generateCmd.Flags().Float64P("seconds", "s", 10.0, "duration of video")
	generateCmd.Flags().StringP("text", "t", "test video", "text to superimpose")
	generateCmd.Flags().StringP("output", "o", "", "output file path")
	_ = uploadCmd.MarkFlagRequired("output")
}

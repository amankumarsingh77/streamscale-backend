package encoder

import (
	"context"
	"fmt"
	"github.com/amankumarsingh77/streamscale-backend/internal/common/entities"
	"github.com/amankumarsingh77/streamscale-backend/internal/worker/utils"
	"os"
	"os/exec"
)

type Encoder struct {
	FFmpegPath    string
	DefaultLadder []entities.VideoProfile
	MaxWorkers    int
}

type EncodeParams struct {
	InputFile  string
	VideoCodec string
	entities.Scene
	entities.VideoProfile
	OutputPrefix string
	Preset       string
	AudioCodec   string
	AudioRate    string
}

func NewCommander(ffmpegPath string) *Encoder {
	return &Encoder{
		FFmpegPath: ffmpegPath,
		DefaultLadder: []entities.VideoProfile{
			{Height: 1080, Bitrate: "5000k"},
			{Height: 720, Bitrate: "3000k"},
			{Height: 480, Bitrate: "1500k"},
			{Height: 360, Bitrate: "800k"},
		},
		MaxWorkers: 4,
	}
}

func (c *Encoder) encodeSegment(ctx context.Context, params EncodeParams) error {
	sceneIndex := int(params.StartTime)
	outputFile := fmt.Sprintf("output/%s_scene%d_%dp.mp4", params.OutputPrefix, sceneIndex, params.Height)
	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", params.StartTime),
		"-t", fmt.Sprintf("%.3f", params.Duration),
		"-i", params.InputFile,
		"-c:v", params.VideoCodec,
		"-b:v", params.Bitrate,
		"-maxrate", fmt.Sprintf("%dk", int(float64(utils.ParseKBitrate(params.Bitrate))*1.5)),
		"-bufsize", fmt.Sprintf("%dk", utils.ParseKBitrate(params.Bitrate)*2),
		"-vf", fmt.Sprintf("scale=-2:%d", params.Height),
		"-preset", params.Preset,
		"-profile:v", "high",
		"-level", "4.1",
		"-c:a", params.AudioCodec,
		"-b:a", params.AudioRate,
		outputFile,
	}
	if _, err := os.Stat(params.InputFile); err == nil {
		return fmt.Errorf("input file %s does not exist", params.InputFile)
	}
	cmd := exec.CommandContext(ctx, c.FFmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run ffmpeg: %w, output: %s", err, string(output))
	}
	return nil
}

package encoder

import (
	"context"
	"fmt"
	"github.com/amankumarsingh77/streamscale-backend/internal/common/entities"
	"os/exec"
	"strconv"
	"strings"
)

func (c *Encoder) AnalyzeComplexity(ctx context.Context, inputFile string) (float64, error) {
	args := []string{
		"-i", inputFile,
		"-vf", "select='eq(pict_type\\,I)'",
		"-vsync", "vfr",
		"-f", "null",
		"-",
	}
	cmd := exec.CommandContext(ctx, c.FFmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, err
	}
	outputStr := string(output)
	frameCount := strings.Count(outputStr, "frame=")
	complexityScore := float64(frameCount) / 100
	if complexityScore < 0.5 {
		complexityScore = 0.5
	} else if complexityScore > 2.0 {
		complexityScore = 2.0
	}
	return complexityScore, nil
}

func (c *Encoder) GenerateEncodingLadder(ctx context.Context, inputFile string) ([]entities.VideoProfile, error) {
	complexity, err := c.AnalyzeComplexity(ctx, inputFile)
	if err != nil {
		return nil, err
	}
	var adjustedLadder []entities.VideoProfile
	for _, profile := range c.DefaultLadder {
		bitrateStr := strings.TrimSuffix(profile.Bitrate, "k")
		bitrate, err := strconv.Atoi(bitrateStr)
		if err != nil {
			return nil, fmt.Errorf("invalid bitrate %s", profile.Bitrate)
		}
		adjustedBitrate := int(float64(bitrate) * complexity)
		adjustedLadder = append(adjustedLadder, entities.VideoProfile{
			Height:  profile.Height,
			Bitrate: fmt.Sprintf("%dk", adjustedBitrate),
		})
	}
	return adjustedLadder, nil
}

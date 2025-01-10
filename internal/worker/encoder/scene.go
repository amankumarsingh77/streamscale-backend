package encoder

import (
	"context"
	"fmt"
	"github.com/amankumarsingh77/streamscale-backend/internal/common/entities"
	"os/exec"
	"regexp"
	"strconv"
)

func (c *Encoder) DetectScene(ctx context.Context, inputFile string) ([]entities.Scene, error) {
	args := []string{
		"-i", inputFile,
		"-vf", "select=gt(scene\\,0.3),metadata=print:file=-",
		"-f", "null",
		"-",
	}
	cmd := exec.CommandContext(ctx, c.ffmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to detect scene %w, output: %s", err, string(output))
	}
	var scenes []entities.Scene
	timeRegex := regexp.MustCompile(`pts_time:([0-9.]+)`)
	matches := timeRegex.FindAllStringSubmatch(string(output), -1)
	for i := 0; i < len(matches); i++ {
		startTime, _ := strconv.ParseFloat(matches[i][1], 64)
		endTime, _ := strconv.ParseFloat(matches[i+1][1], 64)
		duration := endTime - startTime
		scenes = append(scenes, entities.Scene{
			StartTime: startTime,
			EndTime:   endTime,
			Duration:  duration,
		})
	}
	return scenes, nil
}

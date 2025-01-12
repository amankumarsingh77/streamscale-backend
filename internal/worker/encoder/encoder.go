package encoder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amankumarsingh77/streamscale-backend/internal/common/entities"
	"github.com/amankumarsingh77/streamscale-backend/internal/worker/utils"
	"github.com/rs/zerolog/log"
)

const (
	defaultPreset     = "medium"
	defaultAudioRate  = "128k"
	defaultVideoCodec = "libx264"
	defaultAudioCodec = "aac"
)

type Encoder struct {
	FFmpegPath    string
	DefaultLadder []entities.VideoProfile
	MaxWorkers    int
}

type EncodeParams struct {
	InputFile    string
	VideoCodec   string
	Scene        entities.Scene
	VideoProfile entities.VideoProfile
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

func (c *Encoder) EncodeSegment(ctx context.Context, params EncodeParams) (string, error) {
	// Validate input file
	if _, err := os.Stat(params.InputFile); err != nil {
		return "", fmt.Errorf("input file %s does not exist: %w", params.InputFile, err)
	}

	// Ensure output directory exists
	outputDir := filepath.Dir("output")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	// Set default values if not provided
	if params.Preset == "" {
		params.Preset = defaultPreset
	}
	if params.AudioRate == "" {
		params.AudioRate = defaultAudioRate
	}
	if params.VideoCodec == "" {
		params.VideoCodec = defaultVideoCodec
	}
	if params.AudioCodec == "" {
		params.AudioCodec = defaultAudioCodec
	}

	sceneIndex := int(params.Scene.StartTime)
	outputFile := fmt.Sprintf("output/%s_scene%d_%dp.mp4",
		params.OutputPrefix,
		sceneIndex,
		params.VideoProfile.Height,
	)

	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", params.Scene.StartTime),
		"-t", fmt.Sprintf("%.3f", params.Scene.Duration),
		"-i", params.InputFile,
		"-c:v", params.VideoCodec,
		"-b:v", params.VideoProfile.Bitrate,
		"-maxrate", fmt.Sprintf("%dk", int(float64(utils.ParseKBitrate(params.VideoProfile.Bitrate))*1.5)),
		"-bufsize", fmt.Sprintf("%dk", utils.ParseKBitrate(params.VideoProfile.Bitrate)*2),
		"-vf", fmt.Sprintf("scale=-2:%d", params.VideoProfile.Height),
		"-preset", params.Preset,
		"-profile:v", "high",
		"-level", "4.1",
		"-c:a", params.AudioCodec,
		"-b:a", params.AudioRate,
		outputFile,
	}

	startTime := time.Now()
	cmd := exec.CommandContext(ctx, c.FFmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg error: %w\noutput: %s", err, string(output))
	}

	duration := time.Since(startTime)
	log.Info().
		Str("output", outputFile).
		Str("duration", duration.String()).
		Int("height", params.VideoProfile.Height).
		Msg("Encoded segment")

	return outputFile, nil
}

func (c *Encoder) ProcessVideo(ctx context.Context, input, outputPrefix string) (*entities.EncodingStats, error) {
	if err := c.validateInput(input); err != nil {
		return nil, err
	}

	stats := &entities.EncodingStats{
		InputFile: input,
		StartTime: time.Now(),
	}

	log.Print("Processing video...")

	scenes, err := c.DetectScene(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("scene detection failed: %w", err)
	}
	stats.Scenes = scenes

	ladder, err := c.GenerateEncodingLadder(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("encoding ladder generation failed: %w", err)
	}
	stats.Profiles = ladder

	results, err := c.encodeWithWorkerPool(ctx, input, outputPrefix, scenes, ladder)
	if err != nil {
		return nil, err
	}

	stats.OutputFiles = results
	stats.EncodingTime = time.Since(stats.StartTime).Seconds()
	return stats, nil
}

func (c *Encoder) validateInput(input string) error {
	if _, err := os.Stat(input); err != nil {
		return fmt.Errorf("input file validation failed: %w", err)
	}
	if _, err := exec.LookPath(c.FFmpegPath); err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}
	return nil
}

func (c *Encoder) encodeWithWorkerPool(ctx context.Context, input, outputPrefix string,
	scenes []entities.Scene, ladder []entities.VideoProfile) ([]string, error) {

	workChan := make(chan EncodeParams)
	resultChan := make(chan struct {
		file string
		err  error
	}, len(scenes)*len(ladder))

	var wg sync.WaitGroup
	for i := 0; i < c.MaxWorkers; i++ {
		wg.Add(1)
		go c.worker(ctx, i, &wg, workChan, resultChan)
	}

	// Send work
	go func() {
		defer close(workChan)
		for _, scene := range scenes {
			for _, profile := range ladder {
				select {
				case workChan <- EncodeParams{
					InputFile:    input,
					Scene:        scene,
					VideoProfile: profile,
					OutputPrefix: outputPrefix,
				}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	// Wait for workers and close result channel
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var outputs []string
	for result := range resultChan {
		if result.err != nil {
			return nil, result.err
		}
		outputs = append(outputs, result.file)
	}

	return outputs, nil
}

func (c *Encoder) worker(ctx context.Context, id int, wg *sync.WaitGroup,
	work <-chan EncodeParams, results chan<- struct {
		file string
		err  error
	}) {
	defer wg.Done()

	for params := range work {
		select {
		case <-ctx.Done():
			return
		default:
			output, err := c.EncodeSegment(ctx, params)
			results <- struct {
				file string
				err  error
			}{output, err}
		}
	}
}

func (c *Encoder) MergeSegments(ctx context.Context, job *entities.EncodingJob) error {
	outputDir := "output"
	mergedFiles := make(map[string]string)

	// Read all files in the output directory
	files, err := os.ReadDir(outputDir)
	if err != nil {
		return fmt.Errorf("failed to read output directory: %w", err)
	}

	// Group segments by resolution
	resolutions := make(map[string][]string)
	for _, file := range files {
		if strings.Contains(file.Name(), "_scene") {
			resolution := extractResolution(file.Name())
			segments := resolutions[resolution]
			segments = append(segments, file.Name())
			resolutions[resolution] = segments
		}
	}
	log.Print("resolution : ", len(resolutions))

	// Process segments for each resolution
	for resolution, segments := range resolutions {
		// Sort segments by scene number
		sort.Slice(segments, func(i, j int) bool {
			return extractSceneNumber(segments[i]) < extractSceneNumber(segments[j])
		})

		// Create a concat file specific to this resolution
		concatFilePath := filepath.Join(outputDir, fmt.Sprintf("concat_%s.txt", resolution))
		log.Print("concatFilePath : ", concatFilePath)
		if err := createConcatList(concatFilePath, segments); err != nil {
			return fmt.Errorf("failed to create concat list for resolution %s: %w", resolution, err)
		}

		// Merge segments into a single file for this resolution
		outputFilePath := filepath.Join(outputDir, fmt.Sprintf("merged_%s.mp4", resolution))
		if err := c.mergeFiles(ctx, concatFilePath, outputFilePath); err != nil {
			return fmt.Errorf("failed to merge files for resolution %s: %w", resolution, err)
		}

		mergedFiles[resolution] = outputFilePath

		log.Printf("Cleaning up temporary files for resolution: %s", resolution)
		if err := os.Remove(concatFilePath); err != nil {
			log.Printf("Failed to delete concat file: %s, error: %v", concatFilePath, err)
		}
		for _, segment := range segments {
			if err := os.Remove(filepath.Join("output", segment)); err != nil {
				log.Printf("Failed to delete segment file: %s, error: %v", segment, err)
			}
		}
	}

	// Update the job with merged output files
	job.OutputFiles = mergedFiles
	return nil
}

func (c *Encoder) mergeFiles(ctx context.Context, listPath, outputPath string) error {
	args := []string{
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", listPath,
		"-c", "copy",
		outputPath,
	}
	log.Print("Merging segments")
	cmd := exec.CommandContext(ctx, c.FFmpegPath, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg merge failed: %w\noutput: %s", err, string(output))
	}
	log.Print("Segments merged successfully")
	return nil
}

func createConcatList(listPath string, segments []string) error {
	f, err := os.Create(listPath)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, segment := range segments {
		fmt.Fprintf(f, "file '%s'\n", segment)
	}
	return nil
}

func extractResolution(filename string) string {
	re := regexp.MustCompile(`_(\d+p)\.mp4$`)
	matches := re.FindStringSubmatch(filename)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

func extractSceneNumber(filename string) int {
	re := regexp.MustCompile(`scene(\d+)_`)
	matches := re.FindStringSubmatch(filename)
	if len(matches) < 2 {
		return 0
	}
	num, _ := strconv.Atoi(matches[1])
	return num
}

package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Scene represents a detected scene in the video
type Scene struct {
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
	Duration  float64 `json:"duration"`
}

// QualityMetrics stores various quality measurements
type QualityMetrics struct {
	VMAF  float64 `json:"vmaf"`
	SSIM  float64 `json:"ssim"`
	PSNR  float64 `json:"psnr"`
	Scene int     `json:"scene"`
}

// Profile represents an encoding profile with height and bitrate
type Profile struct {
	Height  int    `json:"height"`
	Bitrate string `json:"bitrate"`
}

// EncodingStats contains information about the encoding process
type EncodingStats struct {
	InputFile     string                    `json:"input_file"`
	Complexity    float64                   `json:"complexity"`
	EncodingTime  float64                   `json:"encoding_time"`
	Profiles      []Profile                 `json:"profiles"`
	OutputFiles   []string                  `json:"output_files"`
	Scenes        []Scene                   `json:"scenes"`
	QualityReport map[string]QualityMetrics `json:"quality_report"`
}

// PerTitleEncoder handles the per-title encoding logic
type PerTitleEncoder struct {
	FFmpegPath     string
	DefaultLadders []Profile
	MaxWorkers     int
	logger         zerolog.Logger
}

func init() {
	// Pretty print logs in development
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339})

	// Set global log level
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}

// NewPerTitleEncoder creates a new encoder instance with default settings
func NewPerTitleEncoder(ffmpegPath string) *PerTitleEncoder {
	return &PerTitleEncoder{
		FFmpegPath: ffmpegPath,
		DefaultLadders: []Profile{
			{Height: 1080, Bitrate: "5000k"},
			{Height: 720, Bitrate: "3000k"},
			{Height: 480, Bitrate: "1500k"},
			{Height: 360, Bitrate: "800k"},
		},
		MaxWorkers: 4, // Default to 4 parallel encoding workers
		logger: log.With().
			Str("component", "per_title_encoder").
			Str("ffmpeg_path", ffmpegPath).
			Logger(),
	}
}

func (e *PerTitleEncoder) SetLogLevel(level zerolog.Level) {
	e.logger = e.logger.Level(level)
}

// DetectScenes identifies scene changes in the video
func (e *PerTitleEncoder) DetectScenes(inputFile string) ([]Scene, error) {
	logger := e.logger.With().
		Str("function", "DetectScenes").
		Str("input_file", inputFile).
		Logger()

	logger.Debug().Msg("Starting scene detection")
	start := time.Now()
	cmd := exec.Command(
		e.FFmpegPath,
		"-i", inputFile,
		"-vf", "select=gt(scene\\,0.3),metadata=print:file=-",
		"-f", "null",
		"-",
	)

	logger.Trace().Str("command", cmd.String()).Msg("Executing FFmpeg command")

	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error().
			Err(err).
			Str("ffmpeg_output", string(output)).
			Msg("Scene detection failed")
		return nil, fmt.Errorf("failed to detect scenes: %w", err)
	}

	// Parse scene detection output
	var scenes []Scene
	timeRegex := regexp.MustCompile(`pts_time:([\d.]+)`)
	matches := timeRegex.FindAllStringSubmatch(string(output), -1)

	for i := 0; i < len(matches)-1; i++ {
		startTime, _ := strconv.ParseFloat(matches[i][1], 64)
		endTime, _ := strconv.ParseFloat(matches[i+1][1], 64)

		scenes = append(scenes, Scene{
			StartTime: startTime,
			EndTime:   endTime,
			Duration:  endTime - startTime,
		})
	}

	logger.Info().
		Int("scene_count", len(scenes)).
		Dur("duration", time.Since(start)).
		Msg("Completed scene detection")

	return scenes, nil
}

func (e *PerTitleEncoder) ensureVMAFModel() (string, error) {
	// Define the model directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	modelDir := filepath.Join(homeDir, ".vmaf")
	modelPath := filepath.Join(modelDir, "vmaf_v0.6.1.json")

	// Create directory if it doesn't exist
	if err := os.MkdirAll(modelDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create model directory: %w", err)
	}

	// If model doesn't exist, download it
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		// URL for the VMAF model
		modelURL := "https://raw.githubusercontent.com/Netflix/vmaf/master/model/vmaf_v0.6.1.json"

		// Download the model
		resp, err := http.Get(modelURL)
		if err != nil {
			return "", fmt.Errorf("failed to download VMAF model: %w", err)
		}
		defer resp.Body.Close()

		// Create the file
		file, err := os.Create(modelPath)
		if err != nil {
			return "", fmt.Errorf("failed to create model file: %w", err)
		}
		defer file.Close()

		// Copy the content
		if _, err := io.Copy(file, resp.Body); err != nil {
			return "", fmt.Errorf("failed to save model file: %w", err)
		}
	}

	return modelPath, nil
}

// CalculateQualityMetrics measures VMAF, SSIM, and PSNR
//func (e *PerTitleEncoder) CalculateQualityMetrics(inputFile, encodedFile string, scene int) (QualityMetrics, error) {
//	logger := e.logger.With().
//		Str("function", "CalculateQualityMetrics").
//		Str("input_file", inputFile).
//		Str("encoded_file", encodedFile).
//		Int("scene", scene).
//		Logger()
//
//	metrics := QualityMetrics{Scene: scene}
//
//	// Get input video dimensions
//	getDimensionsCmd := exec.Command(
//		"ffprobe",
//		"-v", "error",
//		"-select_streams", "v:0",
//		"-show_entries", "stream=width,height",
//		"-of", "json",
//		inputFile,
//	)
//	var output bytes.Buffer
//	getDimensionsCmd.Stdout = &output
//
//	if err := getDimensionsCmd.Run(); err != nil {
//		logger.Error().
//			Err(err).
//			Msg("Failed to run ffprobe for dimension extraction")
//		return metrics, fmt.Errorf("ffprobe execution failed: %w", err)
//	}
//
//	type FFProbeOutput struct {
//		Streams []struct {
//			Width  int `json:"width"`
//			Height int `json:"height"`
//		} `json:"streams"`
//	}
//
//	var ffprobeOutput FFProbeOutput
//	if err := json.Unmarshal(output.Bytes(), &ffprobeOutput); err != nil {
//		logger.Error().
//			Err(err).
//			Msg("Failed to parse ffprobe JSON output")
//		return metrics, fmt.Errorf("failed to parse ffprobe output: %w", err)
//	}
//
//	if len(ffprobeOutput.Streams) == 0 || ffprobeOutput.Streams[0].Width == 0 || ffprobeOutput.Streams[0].Height == 0 {
//		logger.Error().
//			Msg("Invalid or missing video dimensions in ffprobe output")
//		return metrics, fmt.Errorf("invalid or missing video dimensions")
//	}
//
//	width := ffprobeOutput.Streams[0].Width
//	height := ffprobeOutput.Streams[0].Height
//
//	logger.Debug().Int("width", width).Int("height", height).Msg("Video dimensions extracted")
//
//	vmafCmd := exec.Command(
//		"./vmaf",
//		"-r", inputFile, // Reference video
//		"-d", encodedFile, // Distorted video
//		"-w", fmt.Sprintf("%d", width), // Width of the video
//		"-h", fmt.Sprintf("%d", height), // Height of the video
//		"-p", "420", // Pixel format
//		"-b", fmt.Sprintf("%d", 8), // Bit depth
//		"-o", "vmaf_results.txt",
//	)
//
//	// Execute the VMAF command
//	if err := runCommand(vmafCmd, logger, "VMAF"); err != nil {
//		return metrics, err
//	}
//
//	// Parse the VMAF score from the output file
//	vmafScore, err := parseVMAFScore("vmaf_results.txt", logger)
//	if err != nil {
//		return metrics, err
//	}
//
//	metrics.VMAF = vmafScore
//
//	logger.Info().
//		Float64("vmaf", metrics.VMAF).
//		Msg("VMAF calculation completed")
//
//	// Commands for calculating SSIM and PSNR
//	tempFiles := map[string]string{
//		"ssim": "ssim.log",
//		"psnr": "psnr.log",
//	}
//
//	defer func() {
//		for _, file := range tempFiles {
//			if err := os.Remove(file); err != nil {
//				logger.Warn().Err(err).Str("file", file).Msg("Failed to remove temporary file")
//			}
//		}
//	}()
//
//	return metrics, nil
//}

func runCommand(cmd *exec.Cmd, logger zerolog.Logger, label string) error {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	if err := cmd.Run(); err != nil {
		logger.Error().
			Err(err).
			Str("stderr", stderr.String()).
			Str("stdout", stdout.String()).
			Dur("duration", time.Since(startTime)).
			Msg(fmt.Sprintf("%s calculation failed", label))
		return fmt.Errorf("%s calculation failed: %w", label, err)
	}

	logger.Debug().Dur("duration", time.Since(startTime)).Msg(fmt.Sprintf("%s calculation completed", label))
	return nil
}

func parseVMAFScore(resultFile string, logger zerolog.Logger) (float64, error) {
	data, err := os.ReadFile(resultFile)
	if err != nil {
		logger.Warn().
			Err(err).
			Str("file", resultFile).
			Msg("Failed to read VMAF result file")
		return 0, fmt.Errorf("failed to read VMAF result file: %w", err)
	}

	// The VMAF result file is expected to contain a line like: "VMAF score = <value>"
	re := regexp.MustCompile(`VMAF score = (\d+\.\d+)`)
	match := re.FindStringSubmatch(string(data))
	if match == nil {
		logger.Warn().
			Str("file", resultFile).
			Msg("Failed to parse VMAF score from result file")
		return 0, fmt.Errorf("failed to parse VMAF score from result file")
	}

	vmafScore, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		logger.Warn().
			Str("file", resultFile).
			Err(err).
			Msg("Failed to parse VMAF score as float")
		return 0, fmt.Errorf("failed to parse VMAF score as float: %w", err)
	}

	logger.Debug().Float64("vmaf", vmafScore).Msg("VMAF score parsed successfully")
	return vmafScore, nil
}

func parseMetricFromFile(filePath, regexPattern string, logger zerolog.Logger, metricName string) float64 {
	data, err := os.ReadFile(filePath)
	if err != nil {
		logger.Warn().
			Err(err).
			Str("file", filePath).
			Msg(fmt.Sprintf("Failed to read %s file", metricName))
		return 0
	}

	re := regexp.MustCompile(regexPattern)
	match := re.FindStringSubmatch(string(data))
	if match == nil {
		logger.Warn().
			Str("file", filePath).
			Msg(fmt.Sprintf("Failed to parse %s from file", metricName))
		return 0
	}

	value, _ := strconv.ParseFloat(match[1], 64)
	logger.Debug().Float64(metricName, value).Msg(fmt.Sprintf("%s parsed successfully", metricName))
	return value
}

// AnalyzeComplexity calculates video complexity score
func (e *PerTitleEncoder) AnalyzeComplexity(inputFile string) (float64, error) {

	logger := e.logger.With().
		Str("function", "AnalyzeComplexity").
		Str("input_file", inputFile).
		Logger()

	cmd := exec.Command(
		e.FFmpegPath,
		"-i", inputFile,
		"-vf", "select=eq(pict_type\\,I)",
		"-vsync", "vfr",
		"-f", "null",
		"-",
	)
	logger.Trace().Str("command", cmd.String()).Msg("Executing FFmpeg command")
	start := time.Now()
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error().
			Err(err).
			Str("ffmpeg_output", string(output)).
			Msg("Failed to analyze video complexity")
		return 0, fmt.Errorf("failed to analyze video: %w", err)
	}

	// Calculate complexity based on I-frame frequency
	outputStr := string(output)
	frameCount := strings.Count(outputStr, "frame=")

	// Calculate complexity score (simplified version)
	complexityScore := float64(frameCount) / 100.0

	// Clamp complexity between 0.5 and 2.0
	if complexityScore < 0.5 {
		complexityScore = 0.5
	} else if complexityScore > 2.0 {
		complexityScore = 2.0
	}

	logger.Info().
		Float64("complexity_score", complexityScore).
		Dur("duration", time.Since(start)).
		Int("frame_count", frameCount).
		Msg("Completed complexity analysis")

	return complexityScore, nil
}

// GenerateEncodingLadder creates a custom encoding ladder based on video complexity
func (e *PerTitleEncoder) GenerateEncodingLadder(inputFile string) ([]Profile, error) {
	logger := e.logger.With().
		Str("function", "GenerateEncodingLadder").
		Str("input_file", inputFile).
		Logger()

	logger.Debug().Msg("Starting encoding ladder generation")
	complexity, err := e.AnalyzeComplexity(inputFile)
	if err != nil {

		return nil, fmt.Errorf("failed to analyze complexity: %w", err)
	}

	var adjustedLadder []Profile
	for _, profile := range e.DefaultLadders {
		// Parse original bitrate
		bitrateStr := strings.TrimSuffix(profile.Bitrate, "k")
		bitrate, err := strconv.Atoi(bitrateStr)
		if err != nil {
			logger.Error().
				Err(err).
				Str("bitrate", profile.Bitrate).
				Msg("Invalid bitrate format")
			return nil, fmt.Errorf("invalid bitrate format: %s", profile.Bitrate)
		}

		adjustedBitrate := int(float64(bitrate) * complexity)

		adjustedLadder = append(adjustedLadder, Profile{
			Height:  profile.Height,
			Bitrate: fmt.Sprintf("%dk", adjustedBitrate),
		})
		logger.Debug().
			Int("height", profile.Height).
			Int("original_bitrate", bitrate).
			Int("adjusted_bitrate", adjustedBitrate).
			Msg("Adjusted profile bitrate")
	}

	logger.Info().
		Float64("complexity", complexity).
		Int("profile_count", len(adjustedLadder)).
		Msg("Generated encoding ladder")

	return adjustedLadder, nil
}

func (e *PerTitleEncoder) encodeSegment(ctx context.Context, inputFile string, scene Scene, profile Profile, outputPrefix string) (string, error) {
	logger := e.logger.With().
		Str("function", "encodeSegment").
		Str("input_file", inputFile).
		Float64("scene_start", scene.StartTime).
		Float64("scene_duration", scene.Duration).
		Int("height", profile.Height).
		Str("bitrate", profile.Bitrate).
		Logger()
	sceneIndex := int(scene.StartTime)
	outputFile := fmt.Sprintf("output/%s_scene%d_%dp.mp4", outputPrefix, sceneIndex, profile.Height)

	logger.Debug().
		Str("output_file", outputFile).
		Msg("Starting segment encoding")

	// Verify input file exists
	if _, err := os.Stat(inputFile); os.IsNotExist(err) {
		logger.Error().
			Err(err).
			Msg("Input file does not exist")
		return "", fmt.Errorf("input file does not exist: %w", err)
	}

	// Create FFmpeg command
	cmd := exec.CommandContext(ctx,
		e.FFmpegPath,
		"-y", // Overwrite output without asking
		"-ss", fmt.Sprintf("%.3f", scene.StartTime),
		"-t", fmt.Sprintf("%.3f", scene.Duration),
		"-i", inputFile,
		"-c:v", "libx264",
		"-b:v", profile.Bitrate,
		"-maxrate", fmt.Sprintf("%dk", int(float64(parseKBitrate(profile.Bitrate))*1.5)),
		"-bufsize", fmt.Sprintf("%dk", parseKBitrate(profile.Bitrate)*2),
		"-vf", fmt.Sprintf("scale=-2:%d", profile.Height),
		"-preset", "slow",
		"-profile:v", "high",
		"-level", "4.1",
		"-c:a", "aac",
		"-b:a", "128k",
		outputFile,
	)

	// Log the complete FFmpeg command
	logger.Debug().
		Str("ffmpeg_command", cmd.String()).
		Msg("FFmpeg command constructed")

	// Capture both stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Record start time
	start := time.Now()

	// Execute the command
	err := cmd.Run()
	duration := time.Since(start)

	// Log execution details
	if err != nil {
		logger.Error().
			Err(err).
			Str("stdout", stdout.String()).
			Str("stderr", stderr.String()).
			Dur("duration", duration).
			Str("ffmpeg_path", e.FFmpegPath).
			Msg("FFmpeg encoding failed")

		// Check if FFmpeg exists and is executable
		if _, err := exec.LookPath(e.FFmpegPath); err != nil {
			logger.Error().
				Err(err).
				Msg("FFmpeg not found in PATH")
			return "", fmt.Errorf("ffmpeg not found in PATH: %w", err)
		}

		// Check output directory permissions
		outputDir := filepath.Dir(outputFile)
		if _, err := os.Stat(outputDir); err != nil {
			logger.Error().
				Err(err).
				Str("output_dir", outputDir).
				Msg("Output directory access error")
			return "", fmt.Errorf("output directory error: %w", err)
		}

		return "", fmt.Errorf("failed to encode segment: %w\nstderr: %s", err, stderr.String())
	}

	// Log successful encoding
	logger.Info().
		Str("output_file", outputFile).
		Dur("duration", duration).
		Int64("output_size", getFileSize(outputFile)).
		Msg("Segment encoding completed successfully")

	return outputFile, nil
}

// Helper function to get file size
func getFileSize(filename string) int64 {
	info, err := os.Stat(filename)
	if err != nil {
		return 0
	}
	return info.Size()
}

// Helper function to validate FFmpeg installation
func (e *PerTitleEncoder) validateFFmpeg() error {
	cmd := exec.Command(e.FFmpegPath, "-version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg validation failed: %w\noutput: %s", err, string(output))
	}
	return nil
}

func (e *PerTitleEncoder) ensureOutputDirectory() string {
	outputDir := filepath.Join("output")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		e.logger.Fatal().Err(err).Msg("Failed to create output directory")
	}
	return outputDir
}

func groupVideoSegmentsByResolution(dir string) (map[string][]string, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	// Define regex to match and extract resolution from filenames
	pattern := regexp.MustCompile(`^output\.mp4_scene\d+_(\d+p)\.mp4$`)
	groupedSegments := make(map[string][]string)

	for _, file := range files {
		if !file.IsDir() {
			matches := pattern.FindStringSubmatch(file.Name())
			if matches != nil {
				resolution := matches[1] // Extract resolution (e.g., 360p, 480p)
				fullPath := filepath.Join(dir, file.Name())
				groupedSegments[resolution] = append(groupedSegments[resolution], fullPath)
			}
		}
	}

	return groupedSegments, nil
}

// createSegmentsFile generates a file listing the video segments
func createSegmentsFile(filename string, segments []string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, segment := range segments {
		_, err := file.WriteString(fmt.Sprintf("file '%s'\n", segment))
		if err != nil {
			return err
		}
	}
	return nil
}

// mergeSegments uses FFmpeg to concatenate video segments
func mergeSegments(segmentsFile, outputFile string) error {
	cmd := exec.Command(
		"ffmpeg",
		"-f", "concat",
		"-safe", "0",
		"-i", segmentsFile,
		"-c", "copy",
		outputFile,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ProcessVideo handles the complete encoding workflow with parallel processing
func (e *PerTitleEncoder) ProcessVideo(ctx context.Context, inputFile, outputPrefix string) (*EncodingStats, error) {
	logger := e.logger.With().
		Str("function", "ProcessVideo").
		Str("input_file", inputFile).
		Str("output_prefix", outputPrefix).
		Logger()
	logger.Info().Msg("Starting video processing")
	processStart := time.Now()
	stats := &EncodingStats{
		InputFile:     inputFile,
		QualityReport: make(map[string]QualityMetrics),
	}

	// Detect scenes
	scenes, err := e.DetectScenes(inputFile)
	if err != nil {
		return nil, fmt.Errorf("scene detection failed: %w", err)
	}
	stats.Scenes = scenes

	// Generate encoding ladder
	ladder, err := e.GenerateEncodingLadder(inputFile)
	if err != nil {
		return nil, fmt.Errorf("failed to generate encoding ladder: %w", err)
	}
	stats.Profiles = ladder

	// Create work pool for parallel encoding
	type encodingWork struct {
		scene   Scene
		profile Profile
	}

	workChan := make(chan encodingWork)
	resultChan := make(chan struct {
		file    string
		metrics QualityMetrics
		err     error
	})

	logger.Debug().
		Int("worker_count", e.MaxWorkers).
		Int("scene_count", len(scenes)).
		Int("profile_count", len(ladder)).
		Msg("Starting worker pool")

	// Start worker pool
	var wg sync.WaitGroup
	for i := 0; i < e.MaxWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			workerLogger := logger.With().Int("worker_id", workerID).Logger()
			for work := range workChan {
				workerLogger.Debug().
					Float64("scene_start", work.scene.StartTime).
					Int("height", work.profile.Height).
					Msg("Processing work item")
				outFile, err := e.encodeSegment(ctx, inputFile, work.scene, work.profile, outputPrefix)
				if err == nil {
					//metrics, metricsErr := e.CalculateQualityMetrics(inputFile, outFile, int(work.scene.StartTime))
					resultChan <- struct {
						file    string
						metrics QualityMetrics
						err     error
					}{outFile, QualityMetrics{}, nil}
				} else {
					resultChan <- struct {
						file    string
						metrics QualityMetrics
						err     error
					}{"", QualityMetrics{}, err}
				}
			}
			workerLogger.Debug().Msg("Worker completed")
		}(i)
	}

	// Send work to pool
	go func() {
		for _, scene := range scenes {
			for _, profile := range ladder {
				select {
				case workChan <- encodingWork{scene, profile}:
				case <-ctx.Done():
					logger.Warn().Msg("Processing cancelled")
					return
				}
			}
		}
		close(workChan)
	}()

	// Collect results
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Process results
	startTime := time.Now()
	for result := range resultChan {
		if result.err != nil {
			logger.Error().
				Err(result.err).
				Msg("Encoding failed")
			return nil, fmt.Errorf("encoding failed: %w", result.err)
		}
		stats.OutputFiles = append(stats.OutputFiles, result.file)
		stats.QualityReport[result.file] = result.metrics
	}
	stats.EncodingTime = time.Since(startTime).Seconds()

	logger.Info().
		Int("output_count", len(stats.OutputFiles)).
		Dur("total_duration", time.Since(processStart)).
		Msg("Completed video processing")

	return stats, nil
}

// Helper function to parse kbps bitrate
func parseKBitrate(bitrate string) int {
	numStr := strings.TrimSuffix(bitrate, "k")
	num, _ := strconv.Atoi(numStr)
	return num
}


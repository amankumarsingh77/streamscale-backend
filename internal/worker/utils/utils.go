package utils

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func GenerateWorkerID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return hostname + "_" + time.Now().Format("20060102150405")
}

func ParseKBitrate(bitrate string) int {
	numStr := strings.TrimSuffix(bitrate, "k")
	num, _ := strconv.Atoi(numStr)
	return num
}

func GetFileSize(filePath string) int64 {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0
	}
	return info.Size()
}

func ValidateFFmpeg(path string) error {
	cmd := exec.Command(path, "-version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg validation failed: %w\noutput: %s", err, string(output))
	}
	return nil
}

func EnsureOutputDir() string {
	outputDir := filepath.Join("output")
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			log.Fatal(fmt.Errorf("failed to create output directory: %w", err))
		}
	}
	return outputDir
}

func DownloadVideoFile(videourl string) (string, error) {
	outputDir := EnsureOutputDir()
	parsedUrl, err := url.Parse(videourl)
	if err != nil {
		return "", nil
	}
	outputFile := filepath.Join(outputDir, path.Base(parsedUrl.Path))
	log.Print("Downloading video file...")
	cmd := exec.Command("wget", "-O", outputFile, videourl)
	output, err := cmd.CombinedOutput()
	if err != nil {
		err := os.Remove(outputFile)
		log.Fatal(fmt.Errorf("failed to download video file: %w\noutput: %s", err, string(output)))
	}
	return outputFile, nil
}

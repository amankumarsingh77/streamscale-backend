package main

import (
	"context"
	"github.com/joho/godotenv"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amankumarsingh77/streamscale-backend/internal/worker/encoder"
	"github.com/amankumarsingh77/streamscale-backend/internal/worker/utils"
	"github.com/rs/zerolog"
)

const (
	JobQueueKey   = "streamscale:jobs"
	ProcessingKey = "streamscale:processing"
	lockTime      = 30 * time.Second
	pollInterval  = 5 * time.Second
)

func init() {
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatal("error loading .env file")
	}
}
func main() {
	// Setup logger
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	// Initialize encoder
	enc := encoder.NewCommander("ffmpeg")
	if err := utils.ValidateFFmpeg(enc.FFmpegPath); err != nil {
		logger.Fatal().Err(err).Msg("Failed to validate FFmpeg")
	}

	// Create worker
	worker, err := NewWorker(os.Getenv("REDIS_URL"), enc, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create worker")
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start worker
	worker.Start(ctx)

	// Wait for shutdown signal
	<-sigChan
	logger.Info().Msg("Shutting down worker...")
	cancel()
}

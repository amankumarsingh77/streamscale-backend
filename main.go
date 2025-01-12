// cmd/encoder/main.go
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"github.com/amankumarsingh77/streamscale-backend/internal/common/entities"
	"github.com/amankumarsingh77/streamscale-backend/internal/worker/encoder"
	"github.com/amankumarsingh77/streamscale-backend/internal/worker/utils"
	"github.com/rs/zerolog/log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/rs/zerolog"
)

const (
	jobQueueKey      = "video_encoder:jobs"
	processingSetKey = "video_encoder:processing"
	lockTimeout      = 30 * time.Minute
	pollInterval     = 1 * time.Second
)

type EncodingJob struct {
	ID           string `json:"id"`
	InputFile    string `json:"input_file"`
	RemoteUrl    string `json:"remote_url"`
	OutputPrefix string `json:"output_prefix"`
	CreatedAt    string `json:"created_at"`
}

type Worker struct {
	id       string
	redis    *redis.Client
	encoder  *encoder.Encoder
	logger   zerolog.Logger
	stopChan chan struct{}
	wg       sync.WaitGroup
}

func NewWorker(redisURL string, enc *encoder.Encoder, logger zerolog.Logger) (*Worker, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	opt.TLSConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	rdb := redis.NewClient(opt)

	// Test Redis connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &Worker{
		id:       generateWorkerID(),
		redis:    rdb,
		encoder:  enc,
		logger:   logger.With().Str("worker_id", generateWorkerID()).Logger(),
		stopChan: make(chan struct{}),
	}, nil
}

func generateWorkerID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return hostname + "_" + time.Now().Format("20060102150405")
}

func (w *Worker) Start(ctx context.Context) error {
	w.logger.Info().Msg("Starting worker")

	// Start cleanup goroutine
	w.wg.Add(1)
	go w.cleanupStaleJobs(ctx)

	// Main processing loop
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-w.stopChan:
				return
			case <-ticker.C:
				if err := w.processNextJob(ctx); err != nil {
					w.logger.Error().Err(err).Msg("Error processing job")
				}
			}
		}
	}()

	return nil
}

func (w *Worker) Stop() {
	close(w.stopChan)
	w.wg.Wait()
	w.redis.Close()
}

func (w *Worker) pushDemoTask() {
	job := entities.EncodingJob{
		ID:        "demo-job-1",
		Filename:  "sample.mp4",
		MimeType:  "video/mp4",
		RemoteUrl: "https://cdn.streamscale.aksdev.me/youtube.mp4",
		Status:    entities.JobStatusPending,
		Progress:  0,
		CreatedAt: time.Now(),
	}

	// Convert to JSON
	jobBytes, _ := json.Marshal(job)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	log.Print("Job pushed")

	defer cancel()

	// Add to pending queue
	w.redis.RPush(ctx, jobQueueKey, jobBytes)
}

func (w *Worker) processNextJob(ctx context.Context) error {
	// Use RPOPLPUSH to atomically move job from queue to processing list
	//w.pushDemoTask()
	jobData, err := w.redis.RPopLPush(ctx, jobQueueKey, processingSetKey).Result()
	if err == redis.Nil {
		return nil // No jobs available
	}
	if err != nil {
		return err
	}

	// Unmarshal JSON into the job struct
	var job EncodingJob
	if err := json.Unmarshal([]byte(jobData), &job); err != nil {
		// Handle invalid JSON
		w.logger.Printf("Failed to unmarshal job: %v", err)
		// Remove invalid job from processing set
		w.redis.LRem(ctx, "processingSet", 1, jobData)
		return err
	}

	job.InputFile, err = utils.DownloadVideoFile(job.RemoteUrl)
	if err != nil {
		w.logger.Printf("Failed to download video file for job %s: %v", job.ID, err)
	}

	// Process the job
	w.logger.Printf("Processing job: %+v", job)
	stats, err := w.encoder.ProcessVideo(ctx, job.InputFile, job.OutputPrefix)
	if err != nil {
		w.logger.Printf("Failed to process video for job %s: %v", job.ID, err)
	} else {
		w.logger.Printf("Job %s completed successfully: %+v", job.ID, stats)
	}

	// Cleanup
	pipe := w.redis.Pipeline()
	pipe.Del(ctx, "lock:"+job.ID)
	pipe.LRem(ctx, "processingSet", 1, jobData)
	_, err = pipe.Exec(ctx)

	return err
}

func (w *Worker) cleanupStaleJobs(ctx context.Context) {
	defer w.wg.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopChan:
			return
		case <-ticker.C:
			w.logger.Debug().Msg("Running stale jobs cleanup")
			if err := w.performCleanup(ctx); err != nil {
				w.logger.Error().Err(err).Msg("Cleanup failed")
			}
		}
	}
}

func (w *Worker) performCleanup(ctx context.Context) error {
	// Get all jobs in processing set
	jobs, err := w.redis.LRange(ctx, processingSetKey, 0, -1).Result()
	if err != nil {
		return err
	}

	for _, jobData := range jobs {
		var job EncodingJob
		if err := json.Unmarshal([]byte(jobData), &job); err != nil {
			// Remove invalid job
			w.redis.LRem(ctx, processingSetKey, 1, jobData)
			continue
		}

		// Check if lock exists and is not stale
		lockKey := "lock:" + job.ID
		_, err := w.redis.Get(ctx, lockKey).Result()
		if err == redis.Nil {
			// Lock doesn't exist, job is stale
			w.logger.Info().Str("job_id", job.ID).Msg("Found stale job, returning to queue")
			pipe := w.redis.Pipeline()
			pipe.LRem(ctx, processingSetKey, 1, jobData)
			pipe.LPush(ctx, jobQueueKey, jobData)
			_, err = pipe.Exec(ctx)
			if err != nil {
				w.logger.Error().Err(err).Str("job_id", job.ID).Msg("Failed to requeue stale job")
			}
		}
	}

	return nil
}

func main() {
	// Command line flags
	redisURL := "redis://default:AdeNAAIjcDE4YTRiNDQ5NGZjZjU0YzgxODg3OTMwN2ZjZjFhNjYxMXAxMA@popular-badger-55181.upstash.io:6379"
	//ffmpegPath := flag.String("ffmpeg", "ffmpeg", "Path to FFmpeg executable")
	//workers := flag.Int("workers", 4, "Number of parallel encoding workers")
	//flag.Parse()

	//// Initialize logger
	//log := logger.NewLogger()

	//// Initialize encoder
	//cfg := config.NewConfig(*ffmpegPath, *workers)
	enc := encoder.NewCommander("ffmpeg")

	// Create worker
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	worker, err := NewWorker(redisURL, enc, logger)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create worker")
	}

	// Context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start worker
	if err := worker.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to start worker")
	}

	// Handle shutdown gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info().Msg("Shutting down worker...")
	cancel()
	worker.Stop()
	log.Info().Msg("Worker stopped")
}

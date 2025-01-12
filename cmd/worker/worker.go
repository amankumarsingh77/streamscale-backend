package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"sync"

	"github.com/amankumarsingh77/streamscale-backend/internal/common/entities"
	"github.com/amankumarsingh77/streamscale-backend/internal/worker/encoder"
	"github.com/amankumarsingh77/streamscale-backend/internal/worker/utils"
	"github.com/go-redis/redis/v8"
	"github.com/rs/zerolog"
)

const (
	// Queue keys
	jobQueuePrefix   = "streamscale:jobs"
	processingPrefix = "streamscale:processing"
	completedPrefix  = "streamscale:completed"
	failedPrefix     = "streamscale:failed"

	// Job status queues
	pendingQueue    = jobQueuePrefix + ":pending"
	processingQueue = jobQueuePrefix + ":processing"
	completedQueue  = jobQueuePrefix + ":completed"
	failedQueue     = jobQueuePrefix + ":failed"

	// Timings
	jobLockDuration = 30 * time.Minute
)

type Worker struct {
	id       string
	redis    *redis.Client
	encoder  *encoder.Encoder
	logger   zerolog.Logger
	stopChan chan struct{}
	wg       *sync.WaitGroup
}

func NewWorker(redisUrl string, encoder *encoder.Encoder, logger zerolog.Logger) (*Worker, error) {
	// Parse the Redis URL
	opt, err := redis.ParseURL(redisUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	opt.TLSConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Create a Redis client using the parsed options
	redisClient := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	// Generate a worker ID
	workerId := utils.GenerateWorkerID()

	// Return the worker instance
	return &Worker{
		id:       workerId,
		redis:    redisClient,
		encoder:  encoder,
		logger:   logger.With().Str("worker_id", workerId).Logger(),
		stopChan: make(chan struct{}),
		wg:       &sync.WaitGroup{},
	}, nil
}

func (w *Worker) Start(ctx context.Context) {
	w.logger.Info().Msg("Starting worker...")
	w.wg.Add(2)
	go w.processJobs(ctx)
	go w.cleanStaleJobs(ctx)
}

func (w *Worker) Stop() {
	w.logger.Info().Msg("Stopping worker...")
	close(w.stopChan)
	w.wg.Wait()
	w.redis.Close()
}

func (w *Worker) processJobs(ctx context.Context) {
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
			processing, err := w.isProcessing(ctx)
			if err != nil {
				w.logger.Error().Err(err).Msg("Failed to check processing status")
				continue
			}
			if processing {
				continue // Skip if already processing a job
			}

			// Try to get and process next job
			if err := w.handleNextJob(ctx); err != nil {
				if !errors.Is(err, redis.Nil) {
					w.logger.Error().Err(err).Msg("Failed to process job")
				}
			}
		}
	}
}

func (w *Worker) isProcessing(ctx context.Context) (bool, error) {
	// Check if worker has any active jobs
	keys, err := w.redis.Keys(ctx, fmt.Sprintf("%s:%s:*", processingPrefix, w.id)).Result()
	if err != nil {
		return false, err
	}
	return len(keys) > 0, nil
}

func (w *Worker) handleNextJob(ctx context.Context) error {
	result, err := w.redis.BLPop(ctx, pollInterval, pendingQueue).Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get job from queue: %w", err)
	}

	var job entities.EncodingJob
	if err := json.Unmarshal([]byte(result[1]), &job); err != nil {
		return fmt.Errorf("failed to unmarshal job: %w", err)
	}

	locked, err := w.lockJob(ctx, job.ID)
	if err != nil {
		return fmt.Errorf("failed to lock job: %w", err)
	}
	if !locked {
		return nil
	}

	job.Status = entities.JobStatusActive
	job.StartedAt = time.Now()

	processingKey := fmt.Sprintf("%s:%s:%s", processingPrefix, w.id, job.ID)
	log.Print("Processing key : ", processingKey)
	if err := w.redis.Set(ctx, processingKey, job.ID, jobLockDuration).Err(); err != nil {
		return fmt.Errorf("failed to set processing status: %w", err)
	}

	job.InputFile, err = utils.DownloadVideoFile(job.RemoteUrl)

	_, err = w.encoder.ProcessVideo(ctx, job.InputFile, job.ID)
	if err != nil {
		job.Status = entities.JobStatusFailed
		job.Error = err.Error()
	} else {
		job.Status = entities.JobStatusComplete
		err = w.encoder.MergeSegments(ctx, &job)
		if err != nil {
			job.Status = entities.JobStatusFailed
			job.Error = fmt.Sprintf("failed to merge scenes: %v", err)
		}
		log.Println("Job completed successfully")
		job.Progress = 100
	}

	job.CompletedAt = time.Now()

	if err := w.releaseJob(ctx, job); err != nil {
		return fmt.Errorf("failed to release job: %w", err)
	}

	if err := w.redis.Del(ctx, processingKey).Err(); err != nil {
		return fmt.Errorf("failed to remove processing status: %w", err)
	}

	return nil
}

func (w *Worker) moveJobToProcessing(ctx context.Context, job entities.EncodingJob) error {
	pipe := w.redis.Pipeline()
	pipe.LRem(ctx, pendingQueue, 0, job)
	pipe.RPush(ctx, processingQueue, job)
	_, err := pipe.Exec(ctx)
	return err
}

func (w *Worker) moveJobToFinal(ctx context.Context, job entities.EncodingJob) error {
	targetQueue := completedQueue
	if job.Status == entities.JobStatusFailed {
		targetQueue = failedQueue
	}

	pipe := w.redis.Pipeline()
	pipe.LRem(ctx, processingQueue, 0, job)
	pipe.RPush(ctx, targetQueue, job)
	_, err := pipe.Exec(ctx)
	return err
}

func (w *Worker) cleanStaleJobs(ctx context.Context) {
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
			if err := w.releaseStaleJobs(ctx); err != nil {
				w.logger.Error().Err(err).Msg("Failed to clean stale jobs")
			}
		}
	}
}

func (w *Worker) lockJob(ctx context.Context, jobId string) (bool, error) {
	return w.redis.SetNX(ctx,
		fmt.Sprintf("%s:%s", ProcessingKey, jobId),
		w.id,
		jobLockDuration,
	).Result()
}

func (w *Worker) releaseJob(ctx context.Context, job entities.EncodingJob) error {
	jobBytes, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}

	pipe := w.redis.Pipeline()
	pipe.Del(ctx, fmt.Sprintf("%s:%s", ProcessingKey, job.ID))
	pipe.RPush(ctx, fmt.Sprintf("streamscale:jobs:%s", job.Status), jobBytes)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to release job: %w", err)
	}

	return nil
}

func (w *Worker) releaseStaleJobs(ctx context.Context) error {
	keys, err := w.redis.Keys(ctx, fmt.Sprintf("%s:*", ProcessingKey)).Result()
	if err != nil {
		return fmt.Errorf("failed to get processing jobs: %w", err)
	}

	pipe := w.redis.Pipeline()
	for _, key := range keys {
		pipe.Del(ctx, key)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to release stale jobs: %w", err)
	}

	return nil
}

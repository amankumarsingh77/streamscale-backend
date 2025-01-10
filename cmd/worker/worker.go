package main

import (
	"context"
	"github.com/amankumarsingh77/streamscale-backend/internal/worker/utils"
	"github.com/go-redis/redis/v8"
	"github.com/rs/zerolog"
	"sync"
	"time"
)

type Worker struct {
	id       string
	redis    *redis.Client
	encoder  *ecoder.Encoder
	logger   zerolog.Logger
	stopChan chan struct{}
	wg       *sync.WaitGroup
}

func NewWorker(redisUrl string, encoder *ecoder.Encoder, logger zerolog.Logger) (*Worker, error) {
	opt, err := redis.ParseURL(redisUrl)
	if err != nil {
		return nil, err
	}
	redisClient := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	workerId := utils.GenerateWorkerID()
	return &Worker{
		id:      workerId,
		redis:   redisClient,
		encoder: encoder,
		logger:  logger.With().Str("worker_id", workerId).Logger(),
		wg:      &sync.WaitGroup{},
	}, nil
}

func (w *Worker) Start() {
	w.logger.Info().Msg("Starting Worker .....")
	w.wg.Add(1)
	go w
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
			if err := w.encoder
			w.logger.Info().Msg("Cleaning stale jobs .....")
		}
	}
}

//func main() {
//	videoInfo, err := worker.GetVideoInfo("testvideos/big_buck_bunny_1080p_h264.mov")
//	if err != nil {
//		log.Fatal(err)
//	}
//}

package main

import "time"

const (
	JobQueueKey   = "streamscale:jobs"
	ProcessingKey = "streamscale:processing"
	lockTime      = 30 * time.Second
	pollInterval  = 5 * time.Second
)

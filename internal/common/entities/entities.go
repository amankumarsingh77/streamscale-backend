package entities

import (
	"time"
)

type JobStatus string

const (
	JobStatusPending  JobStatus = "pending"
	JobStatusActive   JobStatus = "active"
	JobStatusComplete JobStatus = "complete"
	JobStatusFailed   JobStatus = "failed"
)

type VideoStream struct {
	CodecName string  `json:"codec_name" validate:"required"`
	Width     int     `json:"width" validate:"required,gt=0"`
	Height    int     `json:"height" validate:"required,gt=0"`
	FrameRate float64 `json:"r_frame_rate" validate:"required,gt=0"`
	Bitrate   int64   `json:"bit_rate,omitempty"`
}

type AudioStream struct {
	CodecName string `json:"codec_name" validate:"required"`
	Channels  int    `json:"channels" validate:"required,gt=0"`
	Bitrate   int64  `json:"bit_rate,omitempty"`
}

type FileInfo struct {
	Filename string `json:"filename" validate:"required"`
	Duration string `json:"duration" validate:"required"`
	Size     string `json:"size" validate:"required"`
	Format   string `json:"format,omitempty"`
	Bitrate  int64  `json:"bit_rate,omitempty"`
}

type MediaInfo struct {
	VideoStreams []VideoStream `json:"video_streams" validate:"required,dive"`
	AudioStreams []AudioStream `json:"audio_streams" validate:"required,dive"`
	FileInfo     FileInfo      `json:"file_info" validate:"required"`
}

type EncodingJob struct {
	ID          string            `json:"id" validate:"required"`
	Filename    string            `json:"file_name" validate:"required"`
	InputFile   string            `json:"input_file,omitempty"`
	MimeType    string            `json:"mime_type" validate:"required"`
	RemoteUrl   string            `json:"remote_url" validate:"required,url"`
	Status      JobStatus         `json:"status" validate:"required"`
	Progress    float64           `json:"progress"`
	Error       string            `json:"error,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	StartedAt   time.Time         `json:"started_at,omitempty"`
	CompletedAt time.Time         `json:"completed_at,omitempty"`
	OutputFiles map[string]string `json:"output_files,omitempty"`
}

type Scene struct {
	StartTime float64 `json:"start_time" validate:"required,gte=0"`
	EndTime   float64 `json:"end_time" validate:"required,gtfield=StartTime"`
	Duration  float64 `json:"duration" validate:"required,gt=0"`
}

type VideoProfile struct {
	Height  int    `json:"height" validate:"required,gt=0"`
	Bitrate string `json:"bitrate" validate:"required"`
}

type EncodingConfig struct {
	Preset     string         `json:"preset"`
	VideoCodec string         `json:"video_codec"`
	AudioCodec string         `json:"audio_codec"`
	AudioRate  string         `json:"audio_rate"`
	Profiles   []VideoProfile `json:"profiles" validate:"required,dive"`
	MaxWorkers int            `json:"max_workers" validate:"required,gt=0"`
}

type EncodingStats struct {
	InputFile    string         `json:"input_file" validate:"required"`
	Complexity   float64        `json:"complexity"`
	EncodingTime float64        `json:"encoding_time"`
	Profiles     []VideoProfile `json:"profiles" validate:"required,dive"`
	OutputFiles  []string       `json:"output_files"`
	Scenes       []Scene        `json:"scenes" validate:"required,dive"`
	StartTime    time.Time      `json:"start_time"`
	EndTime      time.Time      `json:"end_time,omitempty"`
	Config       EncodingConfig `json:"config"`
}

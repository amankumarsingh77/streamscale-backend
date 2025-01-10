package entities

type VideoStream struct {
	CodecName string  `json:"codec_name"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	FrameRate float64 `json:"r_frame_rate"`
}

type AudioStream struct {
	CodecName string `json:"codec_name"`
	Channels  int    `json:"channels"`
}

type FileInfo struct {
	Filename string `json:"filename"`
	Duration string `json:"duration"`
	Size     string `json:"size"`
}

type MediaInfo struct {
	VideoStreams []VideoStream `json:"video_streams"`
	AudioStreams []AudioStream `json:"audio_streams"`
	FileInfo     FileInfo      `json:"file_info"`
}

type EncodingJob struct {
	ID        string `json:"id"`
	Filename  string `json:"file_name"`
	MimeType  string `json:"mime_type"`
	RemoteUrl string `json:"remote_url"`
	CreatedAt string `json:"created_at"`
}

type Scene struct {
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
	Duration  float64 `json:"duration"`
}

type VideoProfile struct {
	Height  int    `json:"height"`
	Bitrate string `json:"bitrate"`
}

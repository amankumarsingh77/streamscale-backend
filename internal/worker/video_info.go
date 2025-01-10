package worker

import (
	"bytes"
	"encoding/json"
	"github.com/amankumarsingh77/streamscale-backend/internal/common/entities"
	"log"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

func GetVideoInfo(filePath string) (*entities.MediaInfo, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format", "-show_entries", "stream", "-print_format", "json", filePath)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.Fatal(stderr.String())
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out.String()), &result); err != nil {
		log.Fatal(err)
		return nil, err
	}
	var mediaInfo entities.MediaInfo
	if streams, ok := result["streams"].([]interface{}); ok {
		for _, s := range streams {
			stream := s.(map[string]interface{})
			switch stream["codec_type"] {
			case "video":
				video := entities.VideoStream{
					CodecName: stream["codec_name"].(string),
					Width:     int(stream["width"].(float64)),
					Height:    int(stream["height"].(float64)),
				}
				frameRatestr := stream["r_frame_rate"].(string)
				parts := strings.Split(frameRatestr, "/")
				if len(parts) == 2 {
					numerator, err1 := strconv.Atoi(parts[0])
					denominator, err2 := strconv.Atoi(parts[1])
					if err1 == nil && err2 == nil && denominator != 0 {
						video.FrameRate = math.Round(float64(numerator) / float64(denominator))
					} else {
						log.Println("Invalid frame rate")
						video.FrameRate = 0.0
					}
				}
				mediaInfo.VideoStreams = append(mediaInfo.VideoStreams, video)
			case "audio":
				audio := entities.AudioStream{
					CodecName: stream["codec_name"].(string),
					Channels:  int(stream["channels"].(float64)),
				}
				mediaInfo.AudioStreams = append(mediaInfo.AudioStreams, audio)
			}
		}
	}
	if format, err := result["format"].(map[string]interface{}); err {
		mediaInfo.FileInfo = entities.FileInfo{
			Filename: format["filename"].(string),
			Duration: format["duration"].(string),
			Size:     format["size"].(string),
		}
	}
	return &mediaInfo, nil
}

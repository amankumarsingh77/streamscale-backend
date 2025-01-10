package utils

import (
	"os"
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

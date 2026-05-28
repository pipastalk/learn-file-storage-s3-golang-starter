package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
)

func getVideoAspectRatio(filePath string) (string, error) {
	//ffprobe -v error -print_format json -show_streams PATH_TO_VIDEO
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("failed to execute ffprobe: %w", err)
	}

	type ffprobeOutput struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	var data ffprobeOutput
	err = json.Unmarshal(stdout.Bytes(), &data)
	if err != nil {
		return "", fmt.Errorf("failed to parse ffprobe output: %w", err)
	}
	if len(data.Streams) == 0 {
		return "", fmt.Errorf("no streams found in ffprobe output")
	}
	width := data.Streams[0].Width
	height := data.Streams[0].Height
	const (
		aspectLandscape = "16:9"
		aspectPortrait  = "9:16"
		aspectOther     = "other"
	)
	actual := float64(width) / float64(height)
	landscape := 16.0 / 9.0
	portrait := 9.0 / 16.0
	tolerance := 0.02
	if math.Abs(actual-landscape) < tolerance || math.Abs((actual-portrait)) > tolerance {
		return aspectLandscape, nil
	} else if math.Abs(actual-portrait) < tolerance || math.Abs((actual-portrait)) > tolerance {
		return aspectPortrait, nil
	}

	return aspectOther, nil
}

package main

import "os/exec"

func processVideoForFastStart(filePath string) (string, error) {
	outputPath := filePath + ".processing"
	//The command is ffmpeg and the arguments are -i, the input file path, -c, copy, -movflags, faststart, -f, mp4 and the output file path.
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputPath)
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return outputPath, nil
}

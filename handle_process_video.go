package main

import (
	"bytes"
	"os/exec"
)

func processVideoForFastStart(filePath string) (string, error) {
	outputPath := filePath + ".processing"

	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputPath)
	var buffer bytes.Buffer
	cmd.Stdout = &buffer
	if err := cmd.Run(); err != nil {
		return "", err
	}

	return outputPath, nil
}

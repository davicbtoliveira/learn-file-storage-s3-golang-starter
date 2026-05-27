package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func getAspectRatio(filePath string) (string, error) {
	type Dimensions struct {
		Width int `json:"width"`
		Height int `json:"height"`
	}

	type ffprobeResults struct {
		Streams []Dimensions `json:"streams"`
	}

	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var buffer bytes.Buffer
	cmd.Stdout = &buffer
	if err := cmd.Run(); err != nil {
		return "", err
	}

	var result ffprobeResults
	if err := json.Unmarshal(buffer.Bytes(), &result); err != nil {
		return "", err
	}

	if len(result.Streams) == 0 {
		return "", fmt.Errorf("No video streams found in the output")
	}

	videoStream := result.Streams[0]
	ar := arName(videoStream.Width, videoStream.Height)

	return ar, nil
}

func arName(w, h int) string {
	ratio := float64(w) / float64(h)
    const epsilon = 0.05

    if math.Abs(ratio - 16.0/9.0) <= epsilon {
        return "landscape"
    }
    if math.Abs(ratio - 9.0/16.0) <= epsilon {
        return "portrait"
    }
    return "other"
}

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const maxMemory = 1 << 30
	http.MaxBytesReader(w, r.Body, maxMemory)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	videoMetadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, 404, "Video not found", err)
		return
	}

	if videoMetadata.UserID != userID {
		respondWithError(w, 401, "Unauthorized", err)
		return
	}

	file, _, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to fetch video", err)
		return
	}
	defer file.Close()

	mimeType, _, err := mime.ParseMediaType("video/mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Can't parse mime type", err)
		return
	}

	tmpFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error uploading file", err)
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	io.Copy(tmpFile, file)

	outputPath, err := processVideoForFastStart(tmpFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error when processing video", err)
		return
	}

	processedVideo, err := os.Open(outputPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error when processing video", err)
		return
	}
	defer os.Remove(processedVideo.Name())
	defer processedVideo.Close()

	if _, err := processedVideo.Seek(0, io.SeekStart); err != nil {
		respondWithError(w, http.StatusBadRequest, "Error when processing video", err)
		return
	}

	key := make([]byte, 32)
	rand.Read(key)
	rawURLEncoding := base64.RawURLEncoding
	videoHash := rawURLEncoding.EncodeToString(key)

	ar, err := getAspectRatio(processedVideo.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error when fetching video aspect ratio", err)
		return
	}

	s3Key := fmt.Sprintf("%v/%v.mp4", ar, videoHash)
	if _, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &s3Key,
		Body:        processedVideo,
		ContentType: &mimeType,
	}); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error when uploading video to s3", err)
		return
	}

	videoURL := fmt.Sprintf("%v/%v", cfg.s3CfDistribution, s3Key)
	videoMetadata.VideoURL = &videoURL

	if err := cfg.db.UpdateVideo(videoMetadata); err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed while updating videoURL", err)
		return
	}

	respondWithJSON(w, 200, videoMetadata)
}

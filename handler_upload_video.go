package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

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
	tmpFile.Seek(0, io.SeekStart)

	key := make([]byte, 32)
	rand.Read(key)
	rawURLEncoding := base64.RawURLEncoding
	videoHash := rawURLEncoding.EncodeToString(key)

	s3Key := fmt.Sprintf("%v.mp4", videoHash)
	cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &s3Key,
		Body:        tmpFile,
		ContentType: &mimeType,
	})

	videoURL := fmt.Sprintf("https://%v.s3.%v.amazonaws.com/%v", cfg.s3Bucket, cfg.s3Region, s3Key)
	videoMetadata.VideoURL = &videoURL

	if err := cfg.db.UpdateVideo(videoMetadata); err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed while updating videoURL", err)
		return
	}

	respondWithJSON(w, 200, nil)
}

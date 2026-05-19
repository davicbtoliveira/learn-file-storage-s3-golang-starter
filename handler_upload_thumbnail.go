package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't fetch thumbnail from header", err)
		return
	}
	defer file.Close()

	fileType := header.Header.Get("Content-Type")

	videoMetadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Video ID not found in database", err)
		return
	}

	if videoMetadata.UserID != userID {
		respondWithJSON(w, 401, nil)
		return
	}

	if err := cfg.ensureAssetsDir(); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error to ensure assets dir", err)
		return
	}
	mediaType, _, err := mime.ParseMediaType(fileType)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to fetch media type", err)
		return
	}
	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Image format differs from jpeg or png", err)
		return
	}

	key := make([]byte, 32)
	rand.Read(key)
	rawURLEncoding := base64.RawURLEncoding
	videoHash := rawURLEncoding.EncodeToString(key)

	splitMediaType := strings.Split(mediaType, "image/")
	fileName := fmt.Sprintf("%v.%v", videoHash, splitMediaType[1])
	savePath := filepath.Join(cfg.assetsRoot, fileName)
	createFile, err := os.Create(savePath)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error when creating file", err)
		return
	}
	defer createFile.Close()

	if _, err := io.Copy(createFile, file); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error copying file", err)
		return
	}

	newURL := fmt.Sprintf("http://localhost:%v/assets/%v", cfg.port, fileName)
	videoMetadata.ThumbnailURL = &newURL
	cfg.db.UpdateVideo(videoMetadata)

	respondWithJSON(w, http.StatusOK, videoMetadata)
}

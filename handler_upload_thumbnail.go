package main

import (
	"fmt"
	"io"
	"net/http"
	"os"

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

	const maxMemoryBytes = 10 << 20 // 10MB
	err = r.ParseMultipartForm(maxMemoryBytes)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse multipart form", err)
		return
	}

	thumbnailFile, fileHeader, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't get thumbnail file", err)
		return
	}
	defer thumbnailFile.Close()

	mediaType := fileHeader.Header.Get("Content-Type")
	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type for thumbnail", nil)
		return
	}

	assetPath := getAssetPath(videoID, mediaType)
	assetDiskPath := cfg.getAssetDiskPath(assetPath)
	destination, err := os.Create(assetDiskPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create thumbnail file", err)
		return
	}
	defer destination.Close()
	if _, err := io.Copy(destination, thumbnailFile); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to copy thumbnail file", err)
		return
	}

	dbVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Video not found", err)
		return
	}

	if dbVideo.UserID != userID {
		respondWithError(w, http.StatusForbidden, "Not authorized to update this video", nil)
		return
	}

	thumbnailURL := cfg.getAssetURL(assetPath)
	dbVideo.ThumbnailURL = &thumbnailURL

	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, dbVideo)
}

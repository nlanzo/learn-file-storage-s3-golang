package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"

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

	thumbnailData, err := io.ReadAll(thumbnailFile)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to read thumbnail file", err)
		return
	}

	thumbnailDataString := base64.StdEncoding.EncodeToString(thumbnailData)
	thumbnailDataURL := fmt.Sprintf("data:%s;base64,%s", mediaType, thumbnailDataString)

	dbVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Video not found", err)
		return
	}

	if dbVideo.UserID != userID {
		respondWithError(w, http.StatusForbidden, "Not authorized to update this video", nil)
		return
	}

	dbVideo.ThumbnailURL = &thumbnailDataURL

	err = cfg.db.UpdateVideo(dbVideo)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, dbVideo)
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/nlanzo/learn-file-storage-s3-golang/internal/auth"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	http.MaxBytesReader(w, r.Body, 1 << 30) // 1GB

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

	dbVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Video not found", err)
		return
	}

	if dbVideo.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not authorized to update this video", nil)
		return
	}

	videoFile, fileHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't get video file", err)
		return
	}
	defer videoFile.Close()

	mediaType, _, err := mime.ParseMediaType(fileHeader.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type", nil)
		return
	}


	os.MkdirAll(filepath.Join(cfg.assetsRoot, "tmp"), 0755)
	tempVideoFile, err := os.CreateTemp(filepath.Join(cfg.assetsRoot, "tmp"), "tubely-upload-*.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create video file", err)
		return
	}
	defer os.Remove(tempVideoFile.Name())
	defer tempVideoFile.Close()

	if _, err := io.Copy(tempVideoFile, videoFile); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to copy video file", err)
		return
	}

	if _, err := tempVideoFile.Seek(0, io.SeekStart); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to seek video file", err)
		return
	}

	aspectRatio, err := getVideoAspectRatio(tempVideoFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get video aspect ratio", err)
		return
	}
	var prefix string
	switch aspectRatio {
	case "16:9":
		prefix = "landscape"
	case "9:16":
		prefix = "portrait"
	default:
		prefix = "other"
	}

	// process video for fast start
	processedVideoPath, err := processVideoForFastStart(tempVideoFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to process video for fast start", err)
		return
	}
	defer os.Remove(processedVideoPath)
	processedVideoFile, err := os.Open(processedVideoPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to open processed video file", err)
		return
	}
	defer processedVideoFile.Close()

	key := fmt.Sprintf("%s/%s", prefix, getAssetPath(mediaType))
	cfg.s3Client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(cfg.s3Bucket),
		Key:    aws.String(key),
		Body:   processedVideoFile,
		ContentType: aws.String(mediaType),
	})

	videoURL := fmt.Sprintf("https://%s/%s", cfg.s3CfDistribution, key)	
	dbVideo.VideoURL = &videoURL
	cfg.db.UpdateVideo(dbVideo)


	respondWithJSON(w, http.StatusOK, dbVideo)
}

	
func getVideoAspectRatio(filePath string) (string, error) {

	stdoutBuffer := bytes.NewBuffer(nil)
	stderrBuffer := bytes.NewBuffer(nil)
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	cmd.Stdout = stdoutBuffer
	cmd.Stderr = stderrBuffer
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	var videoInfo struct {
		Streams []struct {
			Width int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}
	err = json.Unmarshal(stdoutBuffer.Bytes(), &videoInfo)
	if err != nil {
		return "", err
	}


	if len(videoInfo.Streams) == 0 {
		return "", fmt.Errorf("no streams found")
	}

	// determine aspect ratio is 16:9 or 9:16 or other
  width := videoInfo.Streams[0].Width
	height := videoInfo.Streams[0].Height
	aspectRatio := "other"
	const epsilon = 0.01
	ratio := float64(width) / float64(height)
	if abs(ratio - 16.0/9.0) < epsilon {
		aspectRatio = "16:9"
	} else if abs(ratio - 9.0/16.0) < epsilon {
		aspectRatio = "9:16"
	}

	return aspectRatio, nil
}

func abs(x float64) float64 {
    if x < 0 {
        return -x
    }
    return x
}

func processVideoForFastStart(filePath string) (string, error) {
	outputPath := fmt.Sprintf("%s.processing", filePath)
	err := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputPath).Run()
	if err != nil {
		return "", err
	}
	return outputPath, nil
}
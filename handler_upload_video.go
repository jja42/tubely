package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const maxMemory = 1 << 30

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

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get video data", err)
		return
	}

	if userID != video.UserID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized Request", err)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse multipart form", err)
		return
	}

	contentType := header.Header.Get("Content-Type")

	mediatype, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get media type", err)
		return
	}

	if mediatype != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "invalid media type", err)
		return
	}

	tempfile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to create temp file", err)
		return
	}
	defer os.Remove(tempfile.Name())
	defer tempfile.Close()

	videoData, err := io.ReadAll(file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get video data", err)
		return
	}

	io.Copy(tempfile, bytes.NewReader(videoData))

	tempfile.Seek(0, io.SeekStart)

	ratio, err := getVideoAspectRatio(tempfile.Name())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get video aspect ratio", err)
		return
	}

	var aspectRatio string

	if ratio == "16:9" {
		aspectRatio = "landscape/"
	} else {
		if ratio == "9:16" {
			aspectRatio = "portrait/"
		} else {
			aspectRatio = "other/"
		}
	}

	buffer := make([]byte, 32)
	rand.Read(buffer)
	filekey := base64.RawURLEncoding.EncodeToString(buffer)
	filekey += ".mp4"

	filekey = "videos/" + aspectRatio + filekey

	processedFilePath, err := processVideoForFastStart(tempfile.Name())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to process video", err)
		return
	}

	processedFile, err := os.Open(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to process video", err)
		return
	}
	defer processedFile.Close()

	cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{Bucket: &cfg.s3Bucket, Key: &filekey, Body: processedFile, ContentType: &mediatype})

	videoURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, filekey)

	video.VideoURL = &videoURL

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to update video data", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}

func processVideoForFastStart(filePath string) (string, error) {
	outputPath := filePath + ".processing"

	command := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputPath)

	err := command.Run()
	if err != nil {
		return "", errors.New("unable to process video for fast start")
	}

	return outputPath, nil
}

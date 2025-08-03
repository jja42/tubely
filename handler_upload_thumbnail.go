package main

import (
	"bytes"
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
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")

	mediatype, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get media type", err)
		return
	}

	if mediatype != "image/jpeg" && mediatype != "image/png" {
		respondWithError(w, http.StatusBadRequest, "invalid media type", err)
		return
	}

	imageData, err := io.ReadAll(file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get image data", err)
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

	extension := strings.Split(mediatype, "/")[1]

	thumbnailurl := fmt.Sprintf("http://localhost:%s/assets/%s.%s", cfg.port, videoIDString, extension)

	fmt.Printf("Saving file at %s", thumbnailurl)

	filename := videoIDString + "." + extension
	path := filepath.Join("assets", filename)

	newfile, err := os.Create(path)
	if err != nil {
		fmt.Printf("Filename: %s\n", filename)
		fmt.Printf("Filepath: %s\n", path)
		fmt.Println("Error creating file")
		return
	}

	defer newfile.Close()

	io.Copy(newfile, bytes.NewReader(imageData))

	video.ThumbnailURL = &thumbnailurl

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to update video data", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}

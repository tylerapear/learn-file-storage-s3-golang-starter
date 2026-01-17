package main

import (
	"fmt"
	"net/http"
	"io"
	"os"
	"strings"
	"mime"
	"crypto/rand"
	"encoding/base64"

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
	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse multipart data", err)
		return
	}

	src_file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't get file metadata", err)
		return
	}

	mediaType := header.Header.Get("Content-Type")
	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type for thumbnail", nil)
		return
	}

	parsedMediaType, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse mime type", err)
		return
	}

	if parsedMediaType != "image/jpeg" && parsedMediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type", err)
		return
	}

	video_data, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't find video", err)
		return
	}
	if video_data.UserID != userID {
		respondWithJSON(w, http.StatusUnauthorized, struct{}{})
		return
	}

	file_extension := mediaType[strings.Index(mediaType, "/")+1:]
	dst_file, err := os.Create(fmt.Sprintf("assets/%s.%s", videoID, file_extension))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't create destination file", err)
		return
	}

	_, err = io.Copy(dst_file, src_file)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't copy file", err)
		return
	}

	thumbnail_name_bytes := make([]byte, 32)
	rand.Read(thumbnail_name_bytes)
	fmt.Println(thumbnail_name_bytes)
	thumbnail_name := base64.RawURLEncoding.EncodeToString(thumbnail_name_bytes)
	fmt.Println(thumbnail_name)
	

	new_thumbnail_url := fmt.Sprintf("http://localhost:%s/assets/%s.%s", cfg.port, thumbnail_name, file_extension)
	fmt.Printf(new_thumbnail_url)
	video_data.ThumbnailURL = &new_thumbnail_url
	err = cfg.db.UpdateVideo(video_data)
	if err != nil {
		respondWithError(w, 500, "Couldn't update video thumbnail", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video_data)
	return
}

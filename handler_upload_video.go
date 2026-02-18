package main

import (
	"net/http"
	"mime"
	"io"
	"os"
	"os/exec"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"bytes"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	
	"github.com/google/uuid"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't find video", err)
		return
	}
	if video.UserID != userID {
		respondWithJSON(w, http.StatusUnauthorized, struct{}{})
		return
	}

	uploaded_file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't get file metadata", err)
		return
	}
	defer uploaded_file.Close()

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
	if parsedMediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "File is not an MP4 file", err)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't create temp file", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	io.Copy(tempFile, uploaded_file)
	tempFile.Seek(0, io.SeekStart)
	processedFilePath, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't process temp file", err)
		return
	}
	processedFile, err := os.Open(processedFilePath)
	defer os.Remove(processedFilePath)
	defer processedFile.Close()

	s3key_bytes := make([]byte, 32)
	rand.Read(s3key_bytes)
	video_aspect, err := getVideoAspectRatio(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Could not read aspect ratio", err)
	}
	s3key_prefix := video_aspect
	if video_aspect == "16:9" {
		s3key_prefix = "landscape"
	} else if video_aspect == "9:16" {
		s3key_prefix = "portrait"
	}
	s3key := s3key_prefix + "/" + base64.RawURLEncoding.EncodeToString(s3key_bytes)

	cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket: &cfg.s3Bucket,
		Key: &s3key,
		Body: processedFile,
		ContentType: &parsedMediaType,
	})

	new_video_url := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, s3key)
	video.VideoURL = &new_video_url
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, 500, "Couldn't update video url", err)
		return
	}
}

func getVideoAspectRatio(filePath string) (string, error) {

	command := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var out bytes.Buffer
	command.Stdout = &out
	err := command.Run()
	if err != nil {
		return "", err
	}

	type videoAspect struct {
		Streams []struct {
			Width int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	var data = []byte(out.Bytes())
	var vid videoAspect
	err = json.Unmarshal(data, &vid)
	if err != nil {
		return "", err
	}

	w := vid.Streams[0].Width
	h := vid.Streams[0].Height
	if h <= 0 {
		return "", fmt.Errorf("Bad negative or zero height value")
	} 
	if h <= 0 {
		return "", fmt.Errorf("Bad negative or zero width value")
	}

	ratio := w/h

	if ratio == (16/9) {
		return "16:9", nil
	} 
	if ratio == (9/16) {
		return "9:16", nil
	}

	return "other", nil

}

func processVideoForFastStart(filePath string) (string, error) {

	newFilePath := filePath + ".processing"
	command := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", newFilePath)
	err := command.Run()
	if err != nil {
			return "", err
		}

	return newFilePath, nil

}

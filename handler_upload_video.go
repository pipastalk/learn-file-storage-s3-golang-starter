package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
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
	const maxUploadSize = 1 << 30 // 1GB
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
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video from database", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusForbidden, "You don't have permission to upload a video for this video ID", nil)
		return
	}

	file, _, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't get file from form", err)
		return
	}
	defer file.Close()
	media_type, _, err := mime.ParseMediaType("video/mp4")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid media type", err)
		return
	}
	if media_type != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid media type", nil)
		return
	}

	temp, err := buildTempFileFromUpload(w, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't build temp file from upload", err)
		return
	}
	defer os.Remove(temp.Name()) //first in last out
	defer temp.Close()

	processedFilePath, err := processVideoForFastStart(temp.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't process video for fast start", err)
		return
	}
	defer os.Remove(processedFilePath)
	processedFile, err := os.Open(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't open processed video file", err)
		return
	}
	defer processedFile.Close()
	storageKey := make([]byte, 32)
	rand.Read(storageKey)
	r_string := base64.RawURLEncoding.EncodeToString(storageKey)
	aspectRatio, err := getVideoAspectRatio(temp.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video aspect ratio", err)
		return
	}
	aspectRatioPrefixes := map[string]string{
		"16:9": "landscape",
		"9:16": "portrait",
	}
	aspectRatioPrefix := "other"
	if prefix, ok := aspectRatioPrefixes[aspectRatio]; ok {
		aspectRatioPrefix = prefix
	}
	// https://<bucket-name>.s3.<region>.amazonaws.com/<aspect-ratio-prefix>-<key>
	r_string = fmt.Sprintf("%s/%s", aspectRatioPrefix, r_string)
	url := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, r_string)
	_, err = cfg.s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &r_string,
		Body:        processedFile,
		ContentType: &media_type,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload video to S3", err)
		return
	}

	video.VideoURL = &url
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video in database", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)

}

func buildTempFileFromUpload(w http.ResponseWriter, file io.Reader) (*os.File, error) {
	temp, err := os.CreateTemp("assets/", "tubely-upload-*.mp4")
	if err != nil {
		return nil, errors.New("Couldn't create temp file")
	}
	if _, err = io.Copy(temp, file); err != nil {
		return nil, errors.New("Couldn't write file")
	}
	temp.Seek(0, io.SeekStart)
	return temp, nil
}

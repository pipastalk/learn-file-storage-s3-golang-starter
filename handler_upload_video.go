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
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	video, file, status, msg, err := cfg.prepareVideoUpload(w, r)
	if err != nil {
		respondWithError(w, status, msg, err)
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

	temp, err := buildTempFileFromUpload(file)
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
	r_string, err := buildS3Key(temp)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't build S3 URL", err)
		return
	}
	url := fmt.Sprintf("%s,%s", cfg.s3Bucket, r_string)

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

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	//Use the SDK to create a s3.PresignClient with s3.NewPresignClient
	//Use the client's .PresignGetObject() method with s3.WithPresignExpires as a functional option.
	//Return the .URL field of the v4.PresignedHTTPRequest created by .PresignGetObject()
	client := s3.NewPresignClient(s3Client)
	presignedReq, err := client.PresignGetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}
	return presignedReq.URL, nil
}

func buildS3Key(temp *os.File) (string, error) {
	storageKey := make([]byte, 32)
	rand.Read(storageKey)
	r_string := base64.RawURLEncoding.EncodeToString(storageKey)
	aspectRatio, err := getVideoAspectRatio(temp.Name())
	if err != nil {
		message := "Couldn't get video aspect ratio"
		return "", errors.New(message)
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
	return r_string, nil
}

func (cfg *apiConfig) prepareVideoUpload(w http.ResponseWriter, r *http.Request) (database.Video, io.ReadCloser, int, string, error) {
	const maxUploadSize = 1 << 30 // 1GB
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		msg := "Invalid ID"
		return database.Video{}, nil, http.StatusBadRequest, msg, errors.New(msg)
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		msg := "Couldn't find JWT"
		return database.Video{}, nil, http.StatusUnauthorized, msg, errors.New(msg)
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		msg := "Couldn't validate JWT"
		return database.Video{}, nil, http.StatusUnauthorized, msg, errors.New(msg)
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		msg := "Couldn't get video from database"
		return database.Video{}, nil, http.StatusInternalServerError, msg, errors.New(msg)
	}
	if video.UserID != userID {
		msg := "You don't have permission to upload a video for this video ID"
		return database.Video{}, nil, http.StatusForbidden, msg, errors.New(msg)
	}

	file, _, err := r.FormFile("video")
	if err != nil {
		msg := "Couldn't get file from form"
		return database.Video{}, nil, http.StatusBadRequest, msg, errors.New(msg)
	}

	return video, file, 0, "", nil
}

func buildTempFileFromUpload(file io.Reader) (*os.File, error) {
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

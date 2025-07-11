// domain/video.go
package domain

import "time"

type VideoStatus string

const (
    VideoStatusPending    VideoStatus = "PENDING"
    VideoStatusProcessing VideoStatus = "PROCESSING"
    VideoStatusCompleted  VideoStatus = "COMPLETED"
    VideoStatusFailed     VideoStatus = "FAILED"
)

type Video struct {
    ID               int
    UserID           int
    OriginalFilename string
    Status           VideoStatus
    ProcessedFilePath string
    ErrorMessage     string
    CreatedAt        time.Time
    UpdatedAt        time.Time
}

type VideoProcessingMessage struct {
    UserID            int
    VideoPath         string
    OriginalFilename  string
    ProcessingStarted time.Time
    VideoStatusID     int
}
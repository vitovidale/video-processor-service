// domain/interfaces.go
package domain

type VideoRepository interface {
    Save(video *Video) error
    UpdateStatus(videoID int, status VideoStatus, processedFilePath, errorMessage string) error
    FindByID(videoID int) (*Video, error)
    FindByUserID(userID int) ([]Video, error)
}

type MessageQueueService interface {
    PublishVideoProcessing(message VideoProcessingMessage) error
    ConsumeVideoProcessing(handler func(VideoProcessingMessage)) error
}

type NotificationService interface {
    SendNotification(userID int, originalFilename, status, message string)
}

type FileStorageService interface {
    SaveUploadedFile(src io.Reader, filename string) (string, error)
    GenerateProcessedFileName(userID int, originalFilename string) string
    GenerateFramePattern(outputDir, originalFilename string) string
    DeleteFile(filePath string) error
    DeleteFrames(outputDir, originalFilename string) error
    ZipFrames(outputDir, originalFilename, zipFilePath string) (string, error)
    GetProcessedFilePath(userID int, originalFilename string) string
}

type VideoProcessor interface {
    ExtractFrames(videoPath, framePattern string) error
}
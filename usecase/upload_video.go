// usecase/upload_video.go
package usecase

import (
    "fmt"
    "io"
    "time"
    "log" // Temporário, para logs
    "your_project/domain"
)

type UploadVideoInput struct {
    UserID         int
    FileContent    io.Reader
    OriginalFilename string
}

type UploadVideoOutput struct {
    Message       string
    Filename      string
    VideoStatusID int
}

type UploadVideoUseCase struct {
    VideoRepo      domain.VideoRepository
    MessageQueue   domain.MessageQueueService
    FileStorage    domain.FileStorageService
}

func (uc *UploadVideoUseCase) Execute(input UploadVideoInput) (*UploadVideoOutput, error) {
    // 1. Salvar o arquivo recebido
    uploadDir := "./uploads" // Esta lógica deve estar no FileStorageService
    if _, err := os.Stat(uploadDir); os.IsNotExist(err) {
        os.Mkdir(uploadDir, 0755)
    }
    uniqueFilename := fmt.Sprintf("%d_%s_%d%s", input.UserID, time.Now().Format("20060102150405"), time.Now().UnixNano(), filepath.Ext(input.OriginalFilename))
    filePath := filepath.Join(uploadDir, uniqueFilename)

    if err := c.SaveUploadedFile(file, filePath); err != nil { // C.SaveUploadedFile não pode ser aqui
        return nil, fmt.Errorf("failed to save video file: %w", err)
    }

    // 2. Criar status inicial no DB
    video := &domain.Video{
        UserID:           input.UserID,
        OriginalFilename: input.OriginalFilename,
        Status:           domain.VideoStatusPending,
    }
    if err := uc.VideoRepo.Save(video); err != nil {
        return nil, fmt.Errorf("failed to record video status: %w", err)
    }

    // 3. Publicar mensagem na fila
    message := domain.VideoProcessingMessage{
        UserID:            input.UserID,
        VideoPath:         filePath, // Caminho do arquivo salvo
        OriginalFilename:  input.OriginalFilename,
        ProcessingStarted: time.Now(),
        VideoStatusID:     video.ID,
    }
    if err := uc.MessageQueue.PublishVideoProcessing(message); err != nil {
        return nil, fmt.Errorf("failed to queue video for processing: %w", err)
    }

    log.Printf(" [x] Sent message for video: %s (Status ID: %d)", input.OriginalFilename, video.ID)

    return &UploadVideoOutput{
        Message:       "Video uploaded and queued for processing",
        Filename:      input.OriginalFilename,
        VideoStatusID: video.ID,
    }, nil
}

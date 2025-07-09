// infrastructure/gin_handlers.go
package infrastructure

import (
    "your_project/usecase" // Ajuste o caminho do import
    "github.com/gin-gonic/gin"
    "net/http"
    "strconv"
)

type VideoHandlers struct {
    UploadVideoUC    *usecase.UploadVideoUseCase
    ListVideoStatusUC *usecase.ListVideoStatusUseCase // Crie este use case
    DownloadVideoUC  *usecase.DownloadVideoUseCase   // Crie este use case
}

func NewVideoHandlers(uploadUC *usecase.UploadVideoUseCase, listUC *usecase.ListVideoStatusUseCase, downloadUC *usecase.DownloadVideoUseCase) *VideoHandlers {
    return &VideoHandlers{
        UploadVideoUC:    uploadUC,
        ListVideoStatusUC: listUC,
        DownloadVideoUC:  downloadUC,
    }
}

func (h *VideoHandlers) UploadVideoHandler(c *gin.Context) {
    userID := c.MustGet("user_id").(int)
    fileHeader, err := c.FormFile("video")
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Failed to get video file: %v", err)})
        return
    }

    file, err := fileHeader.Open()
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to open uploaded file: %v", err)})
        return
    }
    defer file.Close()

    input := usecase.UploadVideoInput{
        UserID:          userID,
        FileContent:     file,
        OriginalFilename: fileHeader.Filename,
    }

    output, err := h.UploadVideoUC.Execute(input)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"message": output.Message, "filename": output.Filename, "video_status_id": output.VideoStatusID})
}

// Implemente ListVideoStatusHandler e DownloadProcessedVideoHandler similarmente
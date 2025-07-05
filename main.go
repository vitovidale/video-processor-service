// ... (imports e variáveis globais) ...
package main

import (
	"archive/zip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	amqp "github.com/rabbitmq/amqp091-go"
	jwt "github.com/golang-jwt/jwt/v5"
)

var db *sql.DB
var rabbitMQConn *amqp.Connection
var jwtSecret = []byte(os.Getenv("JWT_SECRET"))

func init() {
    if len(jwtSecret) == 0 {
        log.Println("WARNING: JWT_SECRET environment variable not set. Using a default secret for development. THIS IS INSECURE FOR PRODUCTION!")
        jwtSecret = []byte("supersecretjwtkeythatshouldbeverylongandrandominproduction")
    }
}

type Claims struct {
	Username string `json:"username"`
	UserID   int    `json:"user_id"`
	jwt.RegisteredClaims
}

type VideoProcessingMessage struct {
	UserID            int    `json:"user_id"`
	VideoPath         string `json:"video_path"`
	OriginalFilename  string `json:"original_filename"`
	ProcessingStarted time.Time `json:"processing_started"`
	VideoStatusID     int    `json:"video_status_id"`
}

// NOVA STRUCT para a resposta da listagem de status
type VideoStatusResponse struct {
    ID               int       `json:"id"`
    OriginalFilename string    `json:"original_filename"`
    Status           string    `json:"status"`
    ProcessedFilePath string   `json:"processed_file_path,omitempty"` // omitempty para não incluir se for nulo
    ErrorMessage     string    `json:"error_message,omitempty"`     // omitempty para não incluir se for nulo
    CreatedAt        time.Time `json:"created_at"`
    UpdatedAt        time.Time `json:"updated_at"`
}

// ... (failOnError, initDB, initRabbitMQ, authMiddleware, uploadVideo - SEM MUDANÇAS AQUI) ...

// NOVA FUNÇÃO: Handler para listar status de vídeos de um usuário
func listVideosStatus(c *gin.Context) {
    userID := c.MustGet("user_id").(int) // Obtém o ID do usuário do JWT

    rows, err := db.Query(`SELECT id, video_original_filename, status, processed_file_path, error_message, created_at, updated_at FROM video_processing_statuses WHERE user_id = $1 ORDER BY created_at DESC`, userID)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to query video statuses: %v", err)})
        return
    }
    defer rows.Close()

    var statuses []VideoStatusResponse
    for rows.Next() {
        var s VideoStatusResponse
        var processedFilePath, errorMessage sql.NullString // Usar sql.NullString para campos que podem ser NULL
        err := rows.Scan(&s.ID, &s.OriginalFilename, &s.Status, &processedFilePath, &errorMessage, &s.CreatedAt, &s.UpdatedAt)
        if err != nil {
            log.Printf("Error scanning video status row: %v", err)
            continue
        }
        s.ProcessedFilePath = processedFilePath.String
        s.ErrorMessage = errorMessage.String
        statuses = append(statuses, s)
    }

    if err = rows.Err(); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Error iterating over video statuses: %v", err)})
        return
    }

    c.JSON(http.StatusOK, statuses)
}

// ... (updateVideoStatus, sendNotification, startConsumer, healthCheck - SEM MUDANÇAS AQUI) ...

func main() {
	initDB()
	defer db.Close()

	initRabbitMQ()
	defer rabbitMQConn.Close()

	go startConsumer()

	router := gin.Default()

	router.GET("/health", healthCheck)
	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "Video Processor Service is running!"})
	})

	authRoutes := router.Group("/")
	authRoutes.Use(authMiddleware())
	{
		authRoutes.POST("/upload", uploadVideo)
        authRoutes.GET("/videos/status", listVideosStatus) // <--- NOVA ROTA: Listar status de vídeos
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "5001"
	}
	log.Printf("Video Processor Service escutando na porta :%s...", port)
	log.Fatal(router.Run(":" + port))
}
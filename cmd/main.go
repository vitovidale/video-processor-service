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
	"strconv"
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

type VideoStatusResponse struct {
    ID               int       `json:"id"`
    OriginalFilename string    `json:"original_filename"`
    Status           string    `json:"status"`
    ProcessedFilePath string   `json:"processed_file_path,omitempty"`
    ErrorMessage     string    `json:"error_message,omitempty"`
    CreatedAt        time.Time `json:"created_at"`
    UpdatedAt        time.Time `json:"updated_at"`
}

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}

func initDB() {
	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" {
		dbHost = "db"
	}
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "fiap_x_db"
	}
	dbUser := os.Getenv("DB_USER")
	if dbUser == "" {
		dbUser = "user"
	}
	dbPass := os.Getenv("DB_PASS")
	if dbPass == "" {
		dbPass = "password"
	}
	dbPort := os.Getenv("DB_PORT")
	if dbPort == "" {
		dbPort = "5432"
	}

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPass, dbName)

	var err error
	for i := 0; i < 5; i++ {
		db, err = sql.Open("postgres", connStr)
		if err == nil {
			err = db.Ping()
			if err == nil {
				fmt.Println("Conexão com PostgreSQL estabelecida com sucesso!")
				return
			}
		}
		log.Printf("Tentando conectar ao DB novamente em 5s... (%d/5)", i+1)
		time.Sleep(5 * time.Second)
	}
	log.Fatalf("Falha crítica: Não foi possível conectar ao banco de dados após várias tentativas: %v", err)
}

func initRabbitMQ() {
	rabbitMQHost := os.Getenv("RABBITMQ_HOST")
	if rabbitMQHost == "" {
		rabbitMQHost = "rabbitmq"
	}
	rabbitMQUser := os.Getenv("RABBITMQ_USER")
	if rabbitMQUser == "" {
		rabbitMQUser = "guest"
	}
	rabbitMQPass := os.Getenv("RABBITMQ_PASS")
	if rabbitMQPass == "" {
		rabbitMQPass = "guest"
	}
	rabbitMQPort := os.Getenv("RABBITMQ_PORT")
	if rabbitMQPort == "" {
		rabbitMQPort = "5672"
	}

	connString := fmt.Sprintf("amqp://%s:%s@%s:%s/", rabbitMQUser, rabbitMQPass, rabbitMQHost, rabbitMQPort)

	var err error
	for i := 0; i < 5; i++ {
		rabbitMQConn, err = amqp.Dial(connString)
		if err == nil {
			fmt.Println("Conexão com RabbitMQ estabelecida com sucesso!")
			return
		}
		log.Printf("Tentando conectar ao RabbitMQ novamente em 5s... (%d/5)", i+1)
		time.Sleep(5 * time.Second)
	}
	log.Fatalf("Falha crítica: Não foi possível conectar ao RabbitMQ após várias tentativas: %v", err)
}

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := c.GetHeader("Authorization")
		if tokenString == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		if len(tokenString) > 7 && tokenString[:7] == "Bearer " {
			tokenString = tokenString[7:]
		} else {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()
	}
}

func uploadVideo(c *gin.Context) {
	userID := c.MustGet("user_id").(int)

	file, err := c.FormFile("video")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Failed to get video file: %v", err)})
		return
	}

	uploadDir := "./uploads"
	if _, err := os.Stat(uploadDir); os.IsNotExist(err) {
		os.Mkdir(uploadDir, 0755)
	}

	uniqueFilename := fmt.Sprintf("%d_%s_%d%s", userID, time.Now().Format("20060102150405"), time.Now().UnixNano(), filepath.Ext(file.Filename))
	filePath := filepath.Join(uploadDir, uniqueFilename)

	if err := c.SaveUploadedFile(file, filePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to save video file: %v", err)})
		return
	}

	query := `INSERT INTO video_processing_statuses (user_id, video_original_filename, status) VALUES ($1, $2, $3) RETURNING id`
	var videoStatusID int
	err = db.QueryRow(query, userID, file.Filename, "PENDING").Scan(&videoStatusID)
	if err != nil {
		log.Printf("Error inserting initial video status: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to record video status"})
		return
	}

	message := VideoProcessingMessage{
		UserID:            userID,
		VideoPath:         filePath,
		OriginalFilename:  file.Filename,
		ProcessingStarted: time.Now(),
		VideoStatusID:     videoStatusID,
	}

	body, err := json.Marshal(message)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to marshal message: %v", err)})
		return
	}

	ch, err := rabbitMQConn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()

	q, err := ch.QueueDeclare(
		"video_processing_queue",
		true,   // durable
		false,  // delete when unused
		false,  // exclusive
		false,  // no-wait
		nil,    // arguments
	)
	failOnError(err, "Failed to declare a queue")

	err = ch.Publish(
		"",
		q.Name,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		})
	failOnError(err, "Failed to publish a message")
	log.Printf(" [x] Sent message for video: %s (Status ID: %d)", file.Filename, videoStatusID)

	c.JSON(http.StatusOK, gin.H{"message": "Video uploaded and queued for processing", "filename": file.Filename, "video_status_id": videoStatusID})
}

func listVideosStatus(c *gin.Context) {
    userID := c.MustGet("user_id").(int)

    rows, err := db.Query(`SELECT id, video_original_filename, status, processed_file_path, error_message, created_at, updated_at FROM video_processing_statuses WHERE user_id = $1 ORDER BY created_at DESC`, userID)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to query video statuses: %v", err)})
        return
    }
    defer rows.Close()

    var statuses []VideoStatusResponse
    for rows.Next() {
        var s VideoStatusResponse
        var processedFilePath, errorMessage sql.NullString
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

func downloadProcessedVideo(c *gin.Context) {
    userID := c.MustGet("user_id").(int)
    videoIDStr := c.Param("id")

    videoID, err := strconv.Atoi(videoIDStr)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid video ID"})
        return
    }

    var processedFilePath sql.NullString
    var videoUserID int
    query := `SELECT processed_file_path, user_id FROM video_processing_statuses WHERE id = $1 AND status = 'COMPLETED'`
    err = db.QueryRow(query, videoID).Scan(&processedFilePath, &videoUserID)
    if err != nil {
        if err == sql.ErrNoRows {
            c.JSON(http.StatusNotFound, gin.H{"error": "Processed video not found or not completed"})
            return
        }
        c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to query video path: %v", err)})
        return
    }

    if videoUserID != userID {
        c.JSON(http.StatusForbidden, gin.H{"error": "Access denied: Video does not belong to this user"})
        return
    }

    if !processedFilePath.Valid || processedFilePath.String == "" {
        c.JSON(http.StatusNotFound, gin.H{"error": "Processed file path not found or invalid"})
        return
    }

    filePathToServe := filepath.Join("/app", processedFilePath.String)

    if _, err := os.Stat(filePathToServe); os.IsNotExist(err) {
        c.JSON(http.StatusNotFound, gin.H{"error": "Processed file not found on server storage"})
        return
    }

    c.FileAttachment(filePathToServe, filepath.Base(processedFilePath.String))
}


func updateVideoStatus(videoStatusID int, status string, processedFilePath, errorMessage string) {
	query := `UPDATE video_processing_statuses SET status = $1, processed_file_path = $2, error_message = $3, updated_at = NOW() WHERE id = $4`
	_, err := db.Exec(query, status, processedFilePath, errorMessage, videoStatusID)
	if err != nil {
		log.Printf("ERROR: Failed to update video status for ID %d: %v", videoStatusID, err)
	} else {
		log.Printf("Video status ID %d updated to: %s", videoStatusID, status)
	}
}

func sendNotification(userID int, originalFilename, status, message string) {
	log.Printf("NOTIFICAÇÃO para User ID %d - Vídeo '%s' Status: %s. Mensagem: %s", userID, originalFilename, status, message)
}


func startConsumer() {
	ch, err := rabbitMQConn.Channel()
	failOnError(err, "Failed to open a channel for consumer")
	defer ch.Close()

	q, err := ch.QueueDeclare(
		"video_processing_queue",
		true,   // durable
		false,  // delete when unused
		false,  // exclusive
		false,  // no-wait
		nil,    // arguments
	)
	failOnError(err, "Failed to declare a queue for consumer")

	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		true,   // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	failOnError(err, "Failed to register a consumer")

	log.Println(" [*] Waiting for messages. To exit press CTRL+C")
	forever := make(chan bool)

	go func() {
		for d := range msgs {
			log.Printf(" [x] Received a message: %s", d.Body)

			var msg VideoProcessingMessage
			err := json.Unmarshal(d.Body, &msg)
			if err != nil {
				log.Printf("ERROR: Failed to unmarshal message: %v", err)
				continue
			}

			updateVideoStatus(msg.VideoStatusID, "PROCESSING", "", "")
			sendNotification(msg.UserID, msg.OriginalFilename, "PROCESSING", "Seu vídeo está sendo processado.")

			outputDir := fmt.Sprintf("./processed_videos/%d", msg.UserID)
			if _, err := os.Stat(outputDir); os.IsNotExist(err) {
				os.MkdirAll(outputDir, 0755)
			}

			framePattern := filepath.Join(outputDir, fmt.Sprintf("%s_%%04d.png", filepath.Base(msg.OriginalFilename)))
			cmd := exec.Command("ffmpeg", "-i", msg.VideoPath, "-vf", "fps=1", framePattern)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			err = cmd.Run()
			if err != nil {
				errorMessage := fmt.Sprintf("FFmpeg failed to extract frames: %v", err)
				log.Printf("ERROR processing video '%s': %s", msg.OriginalFilename, errorMessage)
				updateVideoStatus(msg.VideoStatusID, "FAILED", "", errorMessage)
				sendNotification(msg.UserID, msg.OriginalFilename, "FAILED", fmt.Sprintf("Falha ao processar vídeo: %s", errorMessage))
				continue
			}

			zipFilename := fmt.Sprintf("%d_%s_processed.zip", msg.UserID, filepath.Base(msg.OriginalFilename))
			zipFilePath := filepath.Join(outputDir, zipFilename)
			newZipFile, err := os.Create(zipFilePath)
			if err != nil {
				errorMessage := fmt.Sprintf("Failed to create zip file: %v", err)
				log.Printf("ERROR zipping frames for '%s': %s", msg.OriginalFilename, errorMessage)
				updateVideoStatus(msg.VideoStatusID, "FAILED", "", errorMessage)
				sendNotification(msg.UserID, msg.OriginalFilename, "FAILED", fmt.Sprintf("Falha ao compactar frames: %s", errorMessage))
				continue
			}
            // Removed defer newZipFile.Close() here

			zipWriter := zip.NewWriter(newZipFile)
            // Removed defer zipWriter.Close() here

			frames, err := filepath.Glob(filepath.Join(outputDir, fmt.Sprintf("%s_*.png", filepath.Base(msg.OriginalFilename))))
            if err != nil {
                errorMessage := fmt.Sprintf("Failed to list frames for zipping: %v", err)
                log.Printf("ERROR: %s", errorMessage)
                updateVideoStatus(msg.VideoStatusID, "FAILED", "", errorMessage)
                sendNotification(msg.UserID, msg.OriginalFilename, "FAILED", "Falha ao listar frames para compactar.")
                continue
            }
			if len(frames) == 0 {
				errorMessage := "No frames extracted to zip."
				log.Printf("WARNING: %s", errorMessage)
				updateVideoStatus(msg.VideoStatusID, "FAILED", "", errorMessage)
				sendNotification(msg.UserID, msg.OriginalFilename, "FAILED", "Nenhum frame extraído para compactar.")
				continue
			}

			for _, framePath := range frames {
				frameFile, err := os.Open(framePath)
				if err != nil {
					log.Printf("WARNING: Could not open frame %s: %v", framePath, err)
					continue
				}
                // Removed defer frameFile.Close() here

                fileInfo, err := frameFile.Stat() // Obtenha FileInfo e erro
                if err != nil {
                    log.Printf("WARNING: Could not get file info for %s: %v", framePath, err)
                    frameFile.Close() // Close file on error
                    continue
                }
                header, err := zip.FileInfoHeader(fileInfo) // Passe apenas fileInfo
                if err != nil {
                    log.Printf("WARNING: Could not get FileInfoHeader for %s: %v", framePath, err)
                    frameFile.Close() // Close file on error
                    continue
                }
                header.Name = filepath.Base(framePath)
                header.Method = zip.Deflate

				writer, err := zipWriter.CreateHeader(header)
				if err != nil {
					log.Printf("WARNING: Could not create entry for %s in zip: %v", framePath, err)
                    frameFile.Close() // Close file on error
					continue
				}
				_, err = io.Copy(writer, frameFile)
				if err != nil {
					log.Printf("WARNING: Could not copy %s to zip: %v", framePath, err)
                    frameFile.Close() // Close file on error
					continue
				}
                frameFile.Close() // Close file after successful copy
			}

            // Correct order to close zip writer and file
            if err := zipWriter.Close(); err != nil {
                errorMessage := fmt.Sprintf("Failed to close zip writer: %v", err)
                log.Printf("ERROR: %s", errorMessage)
                updateVideoStatus(msg.VideoStatusID, "FAILED", "", errorMessage)
                sendNotification(msg.UserID, msg.OriginalFilename, "FAILED", "Falha ao finalizar compactação (zipWriter).")
                continue
            }
            if err := newZipFile.Close(); err != nil {
                errorMessage := fmt.Sprintf("Failed to close zip file: %v", err)
                log.Printf("ERROR: %s", errorMessage)
                updateVideoStatus(msg.VideoStatusID, "FAILED", "", errorMessage)
                sendNotification(msg.UserID, msg.OriginalFilename, "FAILED", "Falha ao fechar arquivo compactado (newZipFile).")
                continue
            }

			os.Remove(msg.VideoPath)
			for _, framePath := range frames {
				os.Remove(framePath)
			}

			updateVideoStatus(msg.VideoStatusID, "COMPLETED", zipFilePath, "")
			sendNotification(msg.UserID, msg.OriginalFilename, "COMPLETED", fmt.Sprintf("Seu vídeo '%s' foi processado com sucesso! Arquivo ZIP disponível em: %s", msg.OriginalFilename, zipFilePath))
		}
	}()

	<-forever
}

func healthCheck(c *gin.Context) {
	dbStatus := "connected"
	if err := db.Ping(); err != nil {
		dbStatus = fmt.Sprintf("error: %v", err)
	}

	rabbitMQStatus := "connected"
	if rabbitMQConn == nil || rabbitMQConn.IsClosed() {
		rabbitMQStatus = "disconnected"
	} else {
		ch, err := rabbitMQConn.Channel()
		if err != nil {
			rabbitMQStatus = fmt.Sprintf("error: %v", err)
		} else {
			ch.Close()
		}
	}

	if dbStatus != "connected" || rabbitMQStatus != "connected" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "DOWN",
			"database": dbStatus,
			"rabbitmq": rabbitMQStatus,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status": "UP",
		"database": dbStatus,
		"rabbitmq": rabbitMQStatus,
	})
}

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
        authRoutes.GET("/videos/status", listVideosStatus)
        authRoutes.GET("/videos/:id/download", downloadProcessedVideo)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "5001"
	}
	log.Printf("Video Processor Service escutando na porta :%s...", port)
	log.Fatal(router.Run(":" + port))
}
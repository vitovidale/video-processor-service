package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	amqp "github.com/rabbitmq/amqp091-go"
	jwt "github.com/golang-jwt/jwt/v5" // Para JWT, consistente com o user-auth-service
)

var db *sql.DB
var rabbitMQConn *amqp.Connection
var jwtSecret = []byte(os.Getenv("JWT_SECRET")) // Chave secreta para JWT. OBRIGATÓRIO definir via variável de ambiente!

func init() {
    // Defina uma chave secreta padrão para desenvolvimento se a variável de ambiente não estiver definida
    if len(jwtSecret) == 0 {
        log.Println("WARNING: JWT_SECRET environment variable not set. Using a default secret for development. THIS IS INSECURE FOR PRODUCTION!")
        jwtSecret = []byte("supersecretjwtkeythatshouldbeverylongandrandominproduction")
    }
}

// Estrutura para o payload do JWT (igual ao user-auth-service)
type Claims struct {
	Username string `json:"username"`
	UserID   int    `json:"user_id"`
	jwt.RegisteredClaims
}

// Estrutura para a mensagem que será enviada para a fila
type VideoProcessingMessage struct {
	UserID            int    `json:"user_id"`
	VideoPath         string `json:"video_path"`
	OriginalFilename  string `json:"original_filename"`
	ProcessingStarted time.Time `json:"processing_started"`
}

// Função auxiliar para tratamento de erros do RabbitMQ
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

// Middleware de autenticação JWT (similar ao do Auth Service)
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

// Handler para upload de vídeo
func uploadVideo(c *gin.Context) {
	userID := c.MustGet("user_id").(int) // Obtém o ID do usuário do JWT

	file, err := c.FormFile("video") // "video" é o nome esperado do campo no formulário multipart
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Failed to get video file: %v", err)})
		return
	}

	// Criar diretório para uploads se não existir
	uploadDir := "./uploads" // Caminho relativo ao WORKDIR do container
	if _, err := os.Stat(uploadDir); os.IsNotExist(err) {
		os.Mkdir(uploadDir, 0755)
	}

	// Gerar um nome único para o arquivo para evitar colisões
	uniqueFilename := fmt.Sprintf("%d_%s_%d%s", userID, time.Now().Format("20060102150405"), time.Now().UnixNano(), filepath.Ext(file.Filename))
	filePath := filepath.Join(uploadDir, uniqueFilename)

	// Salvar o arquivo no disco
	if err := c.SaveUploadedFile(file, filePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to save video file: %v", err)})
		return
	}

	// Criar a mensagem para a fila
	message := VideoProcessingMessage{
		UserID:            userID,
		VideoPath:         filePath,
		OriginalFilename:  file.Filename,
		ProcessingStarted: time.Now(),
	}

	body, err := json.Marshal(message)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to marshal message: %v", err)})
		return
	}

	// Publicar a mensagem na fila RabbitMQ
	ch, err := rabbitMQConn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()

	q, err := ch.QueueDeclare(
		"video_processing_queue", // nome da fila
		true,   // durable (persiste no broker)
		false,  // delete when unused
		false,  // exclusive
		false,  // no-wait
		nil,    // arguments
	)
	failOnError(err, "Failed to declare a queue")

	err = ch.Publish(
		"",     // exchange
		q.Name, // routing key (nome da fila)
		false,  // mandatory
		false,  // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		})
	failOnError(err, "Failed to publish a message")
	log.Printf(" [x] Sent %s", body)

	// Registrar o status inicial no banco de dados
	// (usaremos a função updateVideoStatus futuramente, por enquanto só insere PENDING)
	_, err = db.Exec(`INSERT INTO video_processing_statuses (user_id, video_original_filename, status) VALUES ($1, $2, $3)`,
		userID, file.Filename, "PENDING")
	if err != nil {
		log.Printf("Error inserting initial video status: %v", err)
		// Não retornamos erro HTTP crítico aqui, pois o upload e o envio para fila funcionaram
	}

	c.JSON(http.StatusOK, gin.H{"message": "Video uploaded and queued for processing", "filename": file.Filename})
}

// Função que consome mensagens da fila (simples para demonstração, processamento real virá depois)
func startConsumer() {
	ch, err := rabbitMQConn.Channel()
	failOnError(err, "Failed to open a channel for consumer")
	defer ch.Close()

	q, err := ch.QueueDeclare(
		"video_processing_queue", // mesmo nome da fila usada para publicar
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
			// Aqui você implementaria a lógica de processamento real:
			// 1. Deserializar a mensagem para VideoProcessingMessage
			// 2. Chamar FFmpeg para extrair frames
			// 3. Compactar em ZIP
			// 4. Salvar o ZIP
			// 5. Atualizar o status no DB (COMPLETED/FAILED)
			// 6. Enviar notificação ao usuário
		}
	}()

	<-forever // Mantém o consumidor rodando
}

// Handler para health check
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

	// Inicia o consumidor de mensagens em uma goroutine separada
	go startConsumer()

	router := gin.Default()

	// Rota de Health Check e raiz
	router.GET("/health", healthCheck)
	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "Video Processor Service is running!"})
	})

	// Rotas protegidas por JWT
	// Este é o endpoint para upload de vídeos
	authRoutes := router.Group("/") // Grupo base para rotas protegidas
	authRoutes.Use(authMiddleware())
	{
		authRoutes.POST("/upload", uploadVideo)
		// Futuramente: rotas para listar status de vídeo, download, etc.
	}


	port := os.Getenv("PORT")
	if port == "" {
		port = "5001" // Porta diferente do Auth Service
	}
	log.Printf("Video Processor Service escutando na porta :%s...", port)
	log.Fatal(router.Run(":" + port))
}
package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	amqp "github.com/rabbitmq/amqp091-go" // Renomeia o pacote para 'amqp' para evitar conflito
)

var db *sql.DB
var rabbitMQConn *amqp.Connection

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
	// Tentar conectar ao DB com retries, pois ele pode não estar pronto imediatamente
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
		rabbitMQHost = "rabbitmq" // Nome do serviço no docker-compose
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
	// Tentar conectar ao RabbitMQ com retries
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

// Handler para o health check
func healthCheck(c *gin.Context) {
	// Verifica a conexão com o DB
	dbStatus := "connected"
	if err := db.Ping(); err != nil {
		dbStatus = fmt.Sprintf("error: %v", err)
	}

	// Verifica a conexão com o RabbitMQ
	rabbitMQStatus := "connected"
	if rabbitMQConn == nil || rabbitMQConn.IsClosed() {
		rabbitMQStatus = "disconnected"
	} else {
		// Opcional: Criar um canal para verificar se a conexão está realmente ativa
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
	defer db.Close() // Garante que a conexão com o DB seja fechada

	initRabbitMQ()
	defer rabbitMQConn.Close() // Garante que a conexão com o RabbitMQ seja fechada

	router := gin.Default()

	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "Video Processor Service is running!"})
	})
	router.GET("/health", healthCheck)

	port := os.Getenv("PORT")
	if port == "" {
		port = "5001" // Porta diferente do Auth Service para evitar conflito
	}
	log.Printf("Video Processor Service escutando na porta :%s...", port)
	log.Fatal(router.Run(":" + port))
}
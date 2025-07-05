# Estágio de build
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod .
COPY go.sum .
RUN go mod download 
COPY . . 
RUN CGO_ENABLED=0 GOOS=linux go build -o /video-processor-service .

# Estágio final (imagem de produção menor)
FROM alpine:latest
WORKDIR /app

# Instalar FFmpeg
RUN apk add --no-cache ffmpeg

COPY --from=builder /video-processor-service .
EXPOSE 5001 
CMD ["/app/video-processor-service"]
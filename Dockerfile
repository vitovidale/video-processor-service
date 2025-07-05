# Estágio de build
FROM golang:1.24-alpine AS builder # Use 1.24 para consistência
WORKDIR /app
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /video-processor-service .

# Estágio final (imagem de produção menor)
FROM alpine:latest
WORKDIR /app
COPY --from=builder /video-processor-service .
EXPOSE 5001 # Porta que o serviço vai expor
CMD ["/app/video-processor-service"]
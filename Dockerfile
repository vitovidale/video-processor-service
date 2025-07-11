FROM golang:1.24-alpine AS builder 

RUN apk update && apk add --no-cache ffmpeg

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /app/video-processor-service ./cmd/main.go 

FROM alpine:latest

RUN apk update && apk add --no-cache ffmpeg

WORKDIR /app

COPY --from=builder /app/video-processor-service .

EXPOSE 5001

CMD ["./video-processor-service"]
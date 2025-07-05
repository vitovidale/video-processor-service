# Estágio de build
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /user-auth-service .

# Estágio final (imagem de produção menor)
FROM alpine:latest
WORKDIR /app
COPY --from=builder /user-auth-service .
EXPOSE 5000
CMD ["/app/user-auth-service"]
# video-processor-service

Microsserviço Go responsável por gerenciar o upload e processamento de vídeos.

## Funcionalidades

* Upload de vídeos e enfileiramento para processamento.
* Processamento assíncrono (FFmpeg para frames, compactação ZIP).
* Consulta de status de vídeos.
* Download de vídeos processados.
* Autenticação JWT para rotas protegidas.

## Como Rodar (com Docker Compose)

Este serviço é parte de uma arquitetura maior e é orquestrado via Docker Compose no repositório `video-iac`.

1.  Certifique-se de que o `ffmpeg` está instalado no `Dockerfile` do serviço.
2.  No diretório `video-iac`, execute:
    ```bash
    docker compose up -d --build
    ```

## Endpoints da API

Todas as rotas que exigem autenticação requerem um token JWT válido no cabeçalho `Authorization: Bearer <token>`.

* `GET /`
* `GET /health`
* `POST /upload` (Autenticado)
* `GET /videos/status` (Autenticado)
* `GET /videos/:id/download` (Autenticado)

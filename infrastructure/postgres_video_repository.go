// infrastructure/postgres_video_repository.go
package infrastructure

import (
    "database/sql"
    "fmt"
    "your_project/domain" // Ajuste o caminho do import
)

type PostgresVideoRepository struct {
    DB *sql.DB
}

func NewPostgresVideoRepository(db *sql.DB) *PostgresVideoRepository {
    return &PostgresVideoRepository{DB: db}
}

func (r *PostgresVideoRepository) Save(video *domain.Video) error {
    query := `INSERT INTO video_processing_statuses (user_id, video_original_filename, status) VALUES ($1, $2, $3) RETURNING id`
    err := r.DB.QueryRow(query, video.UserID, video.OriginalFilename, video.Status).Scan(&video.ID)
    return err
}

func (r *PostgresVideoRepository) UpdateStatus(videoID int, status domain.VideoStatus, processedFilePath, errorMessage string) error {
    query := `UPDATE video_processing_statuses SET status = $1, processed_file_path = $2, error_message = $3, updated_at = NOW() WHERE id = $4`
    _, err := r.DB.Exec(query, status, processedFilePath, errorMessage, videoID)
    return err
}

func (r *PostgresVideoRepository) FindByID(videoID int) (*domain.Video, error) {
    var v domain.Video
    var processedFilePath, errorMessage sql.NullString
    query := `SELECT id, user_id, video_original_filename, status, processed_file_path, error_message, created_at, updated_at FROM video_processing_statuses WHERE id = $1`
    err := r.DB.QueryRow(query, videoID).Scan(
        &v.ID, &v.UserID, &v.OriginalFilename, &v.Status,
        &processedFilePath, &errorMessage, &v.CreatedAt, &v.UpdatedAt,
    )
    if err != nil {
        return nil, err
    }
    v.ProcessedFilePath = processedFilePath.String
    v.ErrorMessage = errorMessage.String
    return &v, nil
}

func (r *PostgresVideoRepository) FindByUserID(userID int) ([]domain.Video, error) {
    rows, err := r.DB.Query(`SELECT id, video_original_filename, status, processed_file_path, error_message, created_at, updated_at FROM video_processing_statuses WHERE user_id = $1 ORDER BY created_at DESC`, userID)
    if err != nil {
        return nil, fmt.Errorf("failed to query video statuses: %w", err)
    }
    defer rows.Close()

    var videos []domain.Video
    for rows.Next() {
        var v domain.Video
        var processedFilePath, errorMessage sql.NullString
        err := rows.Scan(&v.ID, &v.OriginalFilename, &v.Status, &processedFilePath, &errorMessage, &v.CreatedAt, &v.UpdatedAt)
        if err != nil {
            log.Printf("Error scanning video status row: %v", err)
            continue
        }
        v.ProcessedFilePath = processedFilePath.String
        v.ErrorMessage = errorMessage.String
        videos = append(videos, v)
    }
    if err = rows.Err(); err != nil {
        return nil, fmt.Errorf("error iterating over video statuses: %w", err)
    }
    return videos, nil
}
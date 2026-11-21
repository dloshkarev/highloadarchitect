package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

type SessionRepository struct {
	pool *pgxpool.Pool
}

func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

func (r *SessionRepository) Create(ctx context.Context, token string, userID string) error {
	err := pgx.BeginFunc(ctx, r.pool, func(transaction pgx.Tx) error {
		const insertQuery = `
			INSERT INTO sessions (token, user_id)
			VALUES ($1, $2)
		`
		if _, execErr := transaction.Exec(ctx, insertQuery, token, userID); execErr != nil {
			return dbErr("insert session", execErr)
		}

		const deleteQuery = `
			DELETE FROM sessions
			WHERE user_id = $1 AND token <> $2
		`
		if _, execErr := transaction.Exec(ctx, deleteQuery, userID, token); execErr != nil {
			return dbErr("delete old sessions", execErr)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("replace session: %w", err)
	}

	return nil
}

// GetUserID возвращает владельца токена. Неизвестный токен — domain.ErrNotFound.
func (r *SessionRepository) GetUserID(ctx context.Context, token string) (string, error) {
	const query = `
		SELECT user_id
		FROM sessions
		WHERE token = $1
	`
	var userID string
	err := r.pool.QueryRow(ctx, query, token).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", dbErr("select session", err)
	}

	return userID, nil
}

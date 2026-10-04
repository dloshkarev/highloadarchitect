package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionRepository struct {
	pool *pgxpool.Pool
}

func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

func (r *SessionRepository) Create(ctx context.Context, token string, userID string) error {
	const query = `
		INSERT INTO sessions (token, user_id)
		VALUES ($1, $2)
	`
	if _, err := r.pool.Exec(ctx, query, token, userID); err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	return nil
}

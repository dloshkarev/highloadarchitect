package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

type FriendRepository struct {
	pool *pgxpool.Pool
}

func NewFriendRepository(pool *pgxpool.Pool) *FriendRepository {
	return &FriendRepository{pool: pool}
}

// Add добавляет друга идемпотентно: повторная вставка той же пары не ошибка.
// Несуществующий пользователь даёт нарушение внешнего ключа, его отдаём как
// domain.ErrNotFound.
func (r *FriendRepository) Add(ctx context.Context, userID string, friendID string) error {
	const query = `
		INSERT INTO friends (user_id, friend_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`

	err := executeWrite(ctx, "insert friend", func() error {
		_, execErr := r.pool.Exec(ctx, query, userID, friendID)
		if execErr != nil {
			return fmt.Errorf("exec query: %w", execErr)
		}

		return nil
	})

	if err != nil && isForeignKeyViolation(err) {
		return domain.ErrNotFound
	}

	return err
}

// Delete удаляет друга идемпотентно: отсутствие пары не ошибка.
func (r *FriendRepository) Delete(ctx context.Context, userID string, friendID string) error {
	const query = `
		DELETE FROM friends
		WHERE user_id = $1 AND friend_id = $2
	`

	return executeWrite(ctx, "delete friend", func() error {
		_, execErr := r.pool.Exec(ctx, query, userID, friendID)
		if execErr != nil {
			return fmt.Errorf("exec query: %w", execErr)
		}

		return nil
	})
}

// Followers возвращает по одному списку друзей на каждого автора из authorIDs:
// map[authorID][]userID — пользователи, у которых автор в друзьях.
// Авторы без друзей отсутствуют в результате. Один запрос на весь батч.
func (r *FriendRepository) Followers(ctx context.Context, authorIDs []string) (map[string][]string, error) {
	if len(authorIDs) == 0 {
		return map[string][]string{}, nil
	}

	const query = `
		SELECT friend_id, user_id
		FROM friends
		WHERE friend_id = ANY($1::uuid[])
	`
	rows, err := r.pool.Query(ctx, query, authorIDs)
	if err != nil {
		return nil, dbErr("select followers", err)
	}
	defer rows.Close()

	followers := make(map[string][]string, len(authorIDs))
	for rows.Next() {
		var authorID, userID string
		if err := rows.Scan(&authorID, &userID); err != nil {
			return nil, dbErr("scan follower", err)
		}
		followers[authorID] = append(followers[authorID], userID)
	}
	if err := rows.Err(); err != nil {
		return nil, dbErr("iterate followers", err)
	}

	return followers, nil
}

// isForeignKeyViolation — SQLSTATE 23503 (foreign_key_violation).
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation
}

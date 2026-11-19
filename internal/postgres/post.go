package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

type PostRepository struct {
	pool *pgxpool.Pool
}

func NewPostRepository(pool *pgxpool.Pool) *PostRepository {
	return &PostRepository{pool: pool}
}

func (r *PostRepository) Create(ctx context.Context, post domain.Post) error {
	const query = `
		INSERT INTO posts (id, author_id, body, created_at)
		VALUES ($1, $2, $3, $4)
	`

	return executeWrite(ctx, "insert post", func() error {
		_, execErr := r.pool.Exec(ctx, query, post.ID, post.AuthorID, post.Text, post.CreatedAt)
		if execErr != nil {
			return fmt.Errorf("exec query: %w", execErr)
		}

		return nil
	})
}

func (r *PostRepository) GetByID(ctx context.Context, postID string) (domain.Post, error) {
	const query = `
		SELECT id, author_id, body, created_at
		FROM posts
		WHERE id = $1
	`
	var post domain.Post
	err := r.pool.QueryRow(ctx, query, postID).Scan(
		&post.ID,
		&post.AuthorID,
		&post.Text,
		&post.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Post{}, domain.ErrPostNotFound
	}
	if err != nil {
		return domain.Post{}, dbErr("select post", err)
	}

	return post, nil
}

// UpdateText обновляет текст, только если пост принадлежит authorID.
// Если запрос не затронул строк, уточняем причину: пост не найден или чужой.
func (r *PostRepository) UpdateText(ctx context.Context, postID string, authorID string, text string) error {
	const query = `
		UPDATE posts
		SET body = $3
		WHERE id = $1 AND author_id = $2
	`
	var tag pgconn.CommandTag
	err := executeWrite(ctx, "update post", func() error {
		var execErr error
		tag, execErr = r.pool.Exec(ctx, query, postID, authorID, text)
		if execErr != nil {
			return fmt.Errorf("exec query: %w", execErr)
		}

		return nil
	})
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	return r.classifyMiss(ctx, postID, authorID)
}

// Delete удаляет пост, только если он принадлежит authorID.
// Если запрос не затронул строк, уточняем причину: пост не найден или чужой.
func (r *PostRepository) Delete(ctx context.Context, postID string, authorID string) error {
	const query = `
		DELETE FROM posts
		WHERE id = $1 AND author_id = $2
	`
	var tag pgconn.CommandTag
	err := executeWrite(ctx, "delete post", func() error {
		var execErr error
		tag, execErr = r.pool.Exec(ctx, query, postID, authorID)
		if execErr != nil {
			return fmt.Errorf("exec query: %w", execErr)
		}

		return nil
	})
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	return r.classifyMiss(ctx, postID, authorID)
}

// classifyMiss вызывается, когда условный UPDATE/DELETE не затронул строк:
// пост либо не существует, либо принадлежит другому пользователю.
func (r *PostRepository) classifyMiss(ctx context.Context, postID string, authorID string) error {
	const query = `
		SELECT author_id
		FROM posts
		WHERE id = $1
	`
	var ownerID string
	err := r.pool.QueryRow(ctx, query, postID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrPostNotFound
	}
	if err != nil {
		return dbErr("select post author", err)
	}
	if ownerID != authorID {
		return domain.ErrForbidden
	}

	// Строка существует и принадлежит автору, но запрос её не затронул:
	// пост удалили между запросами. Для клиента он уже не найден.
	return domain.ErrPostNotFound
}

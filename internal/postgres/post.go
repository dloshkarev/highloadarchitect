package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

type PostRepository struct {
	pool *pgxpool.Pool
}

func NewPostRepository(pool *pgxpool.Pool) *PostRepository {
	return &PostRepository{pool: pool}
}

// CreateWithOutbox вставляет пост и событие new_post в одной транзакции.
// ON CONFLICT DO NOTHING делает повтор транзакции безопасным: пост с тем же
// id не будет вставлен дважды, а дубль outbox-события допустим.
func (r *PostRepository) CreateWithOutbox(
	ctx context.Context,
	post domain.Post,
	event domain.OutboxEvent,
) error {
	const query = `
		INSERT INTO posts (id, author_id, body, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO NOTHING
	`

	return executeWrite(ctx, "insert post with outbox", func() error {
		return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
			if _, execErr := tx.Exec(ctx, query, post.ID, post.AuthorID, post.Text, post.CreatedAt); execErr != nil {
				return fmt.Errorf("insert post: %w", execErr)
			}

			return insertOutbox(ctx, tx, event)
		})
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

// GetByIDs возвращает посты по идентификаторам. Отсутствующие в БД
// идентификаторы просто не попадают в результат.
func (r *PostRepository) GetByIDs(ctx context.Context, ids []string) ([]domain.Post, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	const query = `
		SELECT id, author_id, body, created_at
		FROM posts
		WHERE id = ANY($1::uuid[])
	`
	rows, err := r.pool.Query(ctx, query, ids)
	if err != nil {
		return nil, dbErr("select posts by ids", err)
	}
	defer rows.Close()

	posts := make([]domain.Post, 0, len(ids))
	for rows.Next() {
		var post domain.Post
		if err := rows.Scan(&post.ID, &post.AuthorID, &post.Text, &post.CreatedAt); err != nil {
			return nil, dbErr("scan post by id", err)
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return nil, dbErr("iterate posts by ids", err)
	}

	return posts, nil
}

// Feed возвращает диапазон постов друзей пользователя [offset, offset+limit),
// свежие сверху. Сортировка детерминирована (created_at, id), поэтому соседние
// страницы не пересекаются и не теряют посты.
func (r *PostRepository) Feed(ctx context.Context, userID string, offset int, limit int) ([]domain.Post, error) {
	const query = `
		SELECT p.id, p.author_id, p.body, p.created_at
		FROM posts AS p
		JOIN friends AS f ON f.friend_id = p.author_id
		WHERE f.user_id = $1
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT $2
		OFFSET $3
	`
	rows, err := r.pool.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, dbErr("select feed", err)
	}
	defer rows.Close()

	posts := make([]domain.Post, 0, limit)
	for rows.Next() {
		var post domain.Post
		if err := rows.Scan(&post.ID, &post.AuthorID, &post.Text, &post.CreatedAt); err != nil {
			return nil, dbErr("scan feed post", err)
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return nil, dbErr("iterate feed", err)
	}

	return posts, nil
}

// UpdateTextWithOutbox обновляет текст и вставляет событие edit_post в одной
// транзакции. Событие пишется только если пост действительно обновлён.
// Если запрос не затронул строк, уточняем причину: пост не найден или чужой.
func (r *PostRepository) UpdateTextWithOutbox(
	ctx context.Context,
	postID string,
	authorID string,
	text string,
	event domain.OutboxEvent,
) error {
	const query = `
		UPDATE posts
		SET body = $3
		WHERE id = $1 AND author_id = $2
	`

	var affected int64
	err := executeWrite(ctx, "update post with outbox", func() error {
		return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
			tag, execErr := tx.Exec(ctx, query, postID, authorID, text)
			if execErr != nil {
				return fmt.Errorf("update post: %w", execErr)
			}

			affected = tag.RowsAffected()
			if affected == 0 {
				return nil
			}

			return insertOutbox(ctx, tx, event)
		})
	})

	if err != nil {
		return err
	}

	if affected > 0 {
		return nil
	}

	return r.classifyMiss(ctx, postID, authorID)
}

// DeleteWithOutbox удаляет пост и вставляет событие delete_post в одной
// транзакции. Событие пишется только если пост действительно удалён.
// Если запрос не затронул строк, уточняем причину: пост не найден или чужой.
func (r *PostRepository) DeleteWithOutbox(
	ctx context.Context,
	postID string,
	authorID string,
	event domain.OutboxEvent,
) error {
	const query = `
		DELETE FROM posts
		WHERE id = $1 AND author_id = $2
	`

	var affected int64
	err := executeWrite(ctx, "delete post with outbox", func() error {
		return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
			tag, execErr := tx.Exec(ctx, query, postID, authorID)
			if execErr != nil {
				return fmt.Errorf("delete post: %w", execErr)
			}

			affected = tag.RowsAffected()
			if affected == 0 {
				return nil
			}

			return insertOutbox(ctx, tx, event)
		})
	})

	if err != nil {
		return err
	}

	if affected > 0 {
		return nil
	}

	return r.classifyMiss(ctx, postID, authorID)
}

// insertOutbox вставляет событие в outbox в рамках переданной транзакции.
func insertOutbox(ctx context.Context, tx pgx.Tx, event domain.OutboxEvent) error {
	const query = `
		INSERT INTO outbox (event_type, payload)
		VALUES ($1, $2)
	`
	if _, err := tx.Exec(ctx, query, event.Type, event.Payload); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}

	return nil
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

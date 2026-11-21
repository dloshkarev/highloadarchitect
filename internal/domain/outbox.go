package domain

import "time"

const (
	OutboxEventNewPost    = "new_post"
	OutboxEventEditPost   = "edit_post"
	OutboxEventDeletePost = "delete_post"
)

// OutboxEvent — строка transactional outbox. При записи заполняются Type и
// Payload, поля ID и CreatedAt назначает БД и они заполняются при чтении.
type OutboxEvent struct {
	ID        int64
	Type      string
	Payload   []byte
	CreatedAt time.Time
}

// PublishedPost — сообщение о новом посте для Kafka и fanout-консьюмера.
type PublishedPost struct {
	PostID             string `json:"post_id"`
	AuthorID           string `json:"author_id"`
	CreatedAtUnixMicro int64  `json:"created_at_unix_micro"`
}

// NewPostPayload — payload события new_post.
type NewPostPayload struct {
	PostID             string `json:"post_id"`
	AuthorID           string `json:"author_id"`
	Body               string `json:"body"`
	CreatedAtUnixMicro int64  `json:"created_at_unix_micro"`
}

// EditPostPayload — payload события edit_post.
type EditPostPayload struct {
	PostID   string `json:"post_id"`
	AuthorID string `json:"author_id"`
	Body     string `json:"body"`
}

// DeletePostPayload — payload события delete_post.
type DeletePostPayload struct {
	PostID   string `json:"post_id"`
	AuthorID string `json:"author_id"`
}

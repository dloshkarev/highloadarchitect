package domain

const (
	OutboxEventNewPost    = "new_post"
	OutboxEventEditPost   = "edit_post"
	OutboxEventDeletePost = "delete_post"
)

// OutboxEvent — событие transactional outbox. Идентификатор и время создания
// назначает БД при вставке, поэтому в структуре для записи их нет.
type OutboxEvent struct {
	Type    string
	Payload []byte
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

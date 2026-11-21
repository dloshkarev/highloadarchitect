package redis

import (
	"time"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

// cachedPost — представление поста в Redis. Отдельная структура нужна, чтобы
// формат кэша не зависел от доменной модели и её будущих изменений.
type cachedPost struct {
	ID        string    `json:"id"`
	AuthorID  string    `json:"author_id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

func newCachedPost(post domain.Post) cachedPost {
	return cachedPost{
		ID:        post.ID,
		AuthorID:  post.AuthorID,
		Text:      post.Text,
		CreatedAt: post.CreatedAt,
	}
}

func (p cachedPost) toDomain() domain.Post {
	return domain.Post{
		ID:        p.ID,
		AuthorID:  p.AuthorID,
		Text:      p.Text,
		CreatedAt: p.CreatedAt,
	}
}

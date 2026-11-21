package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
	"github.com/dloshkarev/highloadarchitect/internal/repository"
)

const (
	maxPostTextLength = 2000
	defaultFeedLimit  = 10
	maxFeedLimit      = 100
)

// FeedService — чтение ленты с кэшем. Может быть nil, тогда используются
// только БД-запросы.
type FeedService interface {
	Feed(ctx context.Context, userID string, offset int, limit int) ([]domain.Post, error)
}

type PostService struct {
	posts repository.PostRepository
	feed  FeedService
}

func NewPostService(posts repository.PostRepository, feed FeedService) *PostService {
	return &PostService{posts: posts, feed: feed}
}

func (s *PostService) Create(ctx context.Context, authorID string, text string) (string, error) {
	normalized, err := normalizePostText(text)
	if err != nil {
		return "", err
	}

	post := domain.Post{
		ID:        uuid.NewString(),
		AuthorID:  authorID,
		Text:      normalized,
		CreatedAt: time.Now().UTC(),
	}
	event, err := newOutboxEvent(domain.OutboxEventNewPost, domain.NewPostPayload{
		PostID:             post.ID,
		AuthorID:           post.AuthorID,
		Body:               post.Text,
		CreatedAtUnixMicro: post.CreatedAt.UnixMicro(),
	})
	if err != nil {
		return "", err
	}
	if err := s.posts.CreateWithOutbox(ctx, post, event); err != nil {
		return "", fmt.Errorf("create post: %w", err)
	}

	return post.ID, nil
}

func (s *PostService) Update(ctx context.Context, userID string, postID string, text string) error {
	normalized, err := normalizePostText(text)
	if err != nil {
		return err
	}
	if err := validatePostID(postID); err != nil {
		return err
	}

	event, err := newOutboxEvent(domain.OutboxEventEditPost, domain.EditPostPayload{
		PostID:   postID,
		AuthorID: userID,
		Body:     normalized,
	})

	if err != nil {
		return err
	}

	if err := s.posts.UpdateTextWithOutbox(ctx, postID, userID, normalized, event); err != nil {
		return fmt.Errorf("update post: %w", err)
	}

	return nil
}

func (s *PostService) Delete(ctx context.Context, userID string, postID string) error {
	if err := validatePostID(postID); err != nil {
		return err
	}

	event, err := newOutboxEvent(domain.OutboxEventDeletePost, domain.DeletePostPayload{
		PostID:   postID,
		AuthorID: userID,
	})

	if err != nil {
		return err
	}

	if err := s.posts.DeleteWithOutbox(ctx, postID, userID, event); err != nil {
		return fmt.Errorf("delete post: %w", err)
	}

	return nil
}

func (s *PostService) Get(ctx context.Context, postID string) (domain.Post, error) {
	if err := validatePostID(postID); err != nil {
		return domain.Post{}, err
	}

	post, err := s.posts.GetByID(ctx, postID)
	if err != nil {
		return domain.Post{}, fmt.Errorf("load post: %w", err)
	}

	return post, nil
}

// Feed возвращает диапазон постов друзей пользователя [offset, offset+limit),
// свежие сверху. Отрицательные offset и limit отклоняются, нулевой limit
// заменяется значением по умолчанию, слишком большой ограничивается
// maxFeedLimit, чтобы не выгружать всю ленту.
func (s *PostService) Feed(ctx context.Context, userID string, offset int, limit int) ([]domain.Post, error) {
	if err := validateUserID(userID); err != nil {
		return nil, err
	}

	if offset < 0 {
		return nil, validationError("offset не может быть отрицательным")
	}

	if limit < 0 {
		return nil, validationError("limit не может быть отрицательным")
	}

	if limit == 0 {
		limit = defaultFeedLimit
	}
	limit = min(limit, maxFeedLimit)

	if s.feed != nil {
		posts, err := s.feed.Feed(ctx, userID, offset, limit)
		if err != nil {
			return nil, fmt.Errorf("load feed: %w", err)
		}

		return posts, nil
	}

	posts, err := s.posts.Feed(ctx, userID, offset, limit)
	if err != nil {
		return nil, fmt.Errorf("load feed: %w", err)
	}

	return posts, nil
}

func normalizePostText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", validationError("текст поста не заполнен")
	}
	if strings.ContainsRune(text, 0) {
		return "", validationError("текст поста содержит нулевой символ")
	}
	if utf8.RuneCountInString(text) > maxPostTextLength {
		return "", validationError(fmt.Sprintf("текст поста длиннее %d символов", maxPostTextLength))
	}

	return text, nil
}

func validatePostID(postID string) error {
	if _, err := uuid.Parse(postID); err != nil {
		return validationError("идентификатор поста должен быть UUID")
	}

	return nil
}

// newOutboxEvent сериализует payload и собирает событие transactional outbox.
func newOutboxEvent(eventType string, payload any) (domain.OutboxEvent, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return domain.OutboxEvent{}, fmt.Errorf("marshal outbox payload: %w", err)
	}

	return domain.OutboxEvent{Type: eventType, Payload: data}, nil
}

package service

import (
	"context"
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

type PostService struct {
	posts repository.PostRepository
}

func NewPostService(posts repository.PostRepository) *PostService {
	return &PostService{posts: posts}
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
	if err := s.posts.Create(ctx, post); err != nil {
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
	if err := s.posts.UpdateText(ctx, postID, userID, normalized); err != nil {
		return fmt.Errorf("update post: %w", err)
	}

	return nil
}

func (s *PostService) Delete(ctx context.Context, userID string, postID string) error {
	if err := validatePostID(postID); err != nil {
		return err
	}
	if err := s.posts.Delete(ctx, postID, userID); err != nil {
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

package feed

import (
	"context"
	"fmt"
	"time"

	"github.com/avast/retry-go/v4"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
	rediscache "github.com/dloshkarev/highloadarchitect/internal/redis"
)

const (
	retryDelay    = 100 * time.Millisecond //nolint:unused // используется в putPost, который вызывает Feed на шаге 5.
	retryMaxDelay = 2 * time.Second        //nolint:unused // используется в putPost, который вызывает Feed на шаге 5.
)

// PostSource — часть репозитория постов, нужная для наполнения кэша тел.
type PostSource interface {
	GetByIDs(ctx context.Context, ids []string) ([]domain.Post, error)
}

// Service обслуживает чтение ленты с кэшем в Redis.
type Service struct {
	cache *rediscache.Cache
	posts PostSource
}

func New(cache *rediscache.Cache, posts PostSource) *Service {
	return &Service{cache: cache, posts: posts}
}

// loadPosts собирает тела постов по идентификаторам из кэша ленты.
// Tombstone-посты пропускаются, отсутствующие в кэше тела добираются из БД
// и кладутся в кэш через SET NX. Посты, которых нет и в БД, пропускаются.
// Порядок результата совпадает с порядком ids.
//
//nolint:unused // вызывается из Feed на шаге 5.
func (s *Service) loadPosts(ctx context.Context, ids []string) ([]domain.Post, error) {
	bodies, _, missing, err := s.cache.GetPosts(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get cached posts: %w", err)
	}

	if len(missing) > 0 {
		posts, err := s.posts.GetByIDs(ctx, missing)
		if err != nil {
			return nil, fmt.Errorf("load posts: %w", err)
		}

		for _, post := range posts {
			bodies[post.ID] = post
			if err := s.putPost(ctx, post); err != nil {
				return nil, err
			}
		}
	}

	result := make([]domain.Post, 0, len(ids))
	for _, id := range ids {
		if post, ok := bodies[id]; ok {
			result = append(result, post)
		}
	}

	return result, nil
}

// putPost кладёт тело поста в Redis, повторяя запись бесконечно до успеха
// или отмены контекста.
//
//nolint:unused // вызывается из loadPosts.
func (s *Service) putPost(ctx context.Context, post domain.Post) error {
	err := retry.Do(
		func() error {
			return s.cache.PutPostIfAbsent(ctx, post)
		},
		retry.Attempts(0),
		retry.Delay(retryDelay),
		retry.MaxDelay(retryMaxDelay),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
	)
	if err != nil {
		return fmt.Errorf("cache post %s: %w", post.ID, err)
	}

	return nil
}

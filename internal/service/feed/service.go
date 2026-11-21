package feed

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/avast/retry-go/v4"

	"github.com/dloshkarev/highloadarchitect/internal/config"
	"github.com/dloshkarev/highloadarchitect/internal/domain"
	"github.com/dloshkarev/highloadarchitect/internal/metrics"
	rediscache "github.com/dloshkarev/highloadarchitect/internal/redis"
)

const (
	retryDelay    = 100 * time.Millisecond
	retryMaxDelay = 2 * time.Second
)

// PostSource — часть репозитория постов, нужная для чтения ленты и наполнения кэша.
type PostSource interface {
	Feed(ctx context.Context, userID string, offset int, limit int) ([]domain.Post, error)
	GetByIDs(ctx context.Context, ids []string) ([]domain.Post, error)
}

// Service обслуживает чтение ленты с кэшем в Redis.
type Service struct {
	cache *rediscache.Cache
	posts PostSource
	cfg   config.FeedConfig
}

func New(cache *rediscache.Cache, posts PostSource, cfg config.FeedConfig) *Service {
	return &Service{cache: cache, posts: posts, cfg: cfg}
}

// Feed возвращает диапазон ленты [offset, offset+limit), свежие сверху.
// Если запрошенный диапазон целиком покрыт кэшем, читает из Redis.
// Иначе, а также при ошибках кэша, читает из БД. TTL кэша продлевается
// на каждом успешном чтении.
func (s *Service) Feed(ctx context.Context, userID string, offset int, limit int) ([]domain.Post, error) {
	if !s.cfg.Enabled {
		return s.loadFromDB(ctx, userID, offset, limit)
	}

	card, err := s.cache.Card(ctx, userID)
	if err != nil {
		metrics.FeedCacheMisses.Inc()
		s.logCacheError("read feed card", userID, err)

		return s.loadFromDB(ctx, userID, offset, limit)
	}

	if card == 0 {
		var empty bool
		card, empty, err = s.warmFeed(ctx, userID)
		if err != nil {
			return nil, err
		}
		if empty {
			metrics.FeedCacheMisses.Inc()

			return []domain.Post{}, nil
		}
	}

	if card > 0 && int64(offset) <= card-int64(limit) {
		ids, err := s.cache.Range(ctx, userID, offset, limit)
		if err != nil {
			metrics.FeedCacheMisses.Inc()
			s.logCacheError("read feed range", userID, err)

			return s.loadFromDBAndTouch(ctx, userID, offset, limit)
		}

		posts, err := s.loadPosts(ctx, ids)
		if err != nil {
			metrics.FeedCacheMisses.Inc()
			s.logCacheError("load feed posts", userID, err)

			return s.loadFromDBAndTouch(ctx, userID, offset, limit)
		}

		metrics.FeedCacheHits.Inc()
		s.touch(ctx, userID)

		return posts, nil
	}

	metrics.FeedCacheMisses.Inc()

	return s.loadFromDBAndTouch(ctx, userID, offset, limit)
}

// warmFeed прогревает кэш ленты из БД, когда в Redis пусто, и возвращает
// свежее число постов и признак «в БД у пользователя нет постов».
// Пустая лента не создаёт ключ: Redis не хранит пустые sorted set,
// прогрев повторится на следующем чтении.
func (s *Service) warmFeed(ctx context.Context, userID string) (int64, bool, error) {
	started := time.Now()

	warmed, err := s.posts.Feed(ctx, userID, 0, s.cfg.WarmLimit)
	if err != nil {
		return 0, false, fmt.Errorf("warm feed: %w", err)
	}

	metrics.FeedWarmTotal.Inc()
	metrics.FeedWarmDuration.Observe(time.Since(started).Seconds())

	if len(warmed) == 0 {
		return 0, true, nil
	}

	if err := s.cache.Warm(ctx, userID, warmed); err != nil {
		s.logCacheError("warm feed", userID, err)

		return 0, false, nil
	}

	card, err := s.cache.Card(ctx, userID)
	if err != nil {
		s.logCacheError("read warmed feed card", userID, err)

		return 0, false, nil
	}

	return card, false, nil
}

// loadPosts собирает тела постов по идентификаторам из кэша ленты.
// Tombstone-посты пропускаются, отсутствующие в кэше тела добираются из БД
// и кладутся в кэш через SET NX. Посты, которых нет и в БД, пропускаются.
// Порядок результата совпадает с порядком ids.
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

func (s *Service) loadFromDB(ctx context.Context, userID string, offset int, limit int) ([]domain.Post, error) {
	posts, err := s.posts.Feed(ctx, userID, offset, limit)
	if err != nil {
		return nil, fmt.Errorf("load feed: %w", err)
	}

	return posts, nil
}

func (s *Service) loadFromDBAndTouch(ctx context.Context, userID string, offset int, limit int) ([]domain.Post, error) {
	posts, err := s.loadFromDB(ctx, userID, offset, limit)
	if err != nil {
		return nil, err
	}

	s.touch(ctx, userID)

	return posts, nil
}

// touch продлевает TTL ленты. Ошибка кэша не должна ломать чтение из БД.
func (s *Service) touch(ctx context.Context, userID string) {
	if err := s.cache.Touch(ctx, userID); err != nil {
		s.logCacheError("touch feed", userID, err)
	}
}

// putPost кладёт тело поста в Redis, повторяя запись бесконечно до успеха
// или отмены контекста.
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

func (s *Service) logCacheError(operation string, userID string, err error) {
	slog.Error("feed cache error", "operation", operation, "user_id", userID, "err", err)
}

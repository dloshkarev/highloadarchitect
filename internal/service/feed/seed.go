package feed

import (
	"context"
	"fmt"
	"time"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
	"github.com/dloshkarev/highloadarchitect/internal/metrics"
	rediscache "github.com/dloshkarev/highloadarchitect/internal/redis"
)

// FollowerSource возвращает по одному списку друзей на каждого автора батча.
type FollowerSource interface {
	Followers(ctx context.Context, authorIDs []string) (map[string][]string, error)
}

// FanoutCache добавляет посты в ленты получателей.
type FanoutCache interface {
	Fanout(ctx context.Context, userID string, items []rediscache.FanoutItem) error
}

// Seeder раскладывает батч постов по лентам друзей авторов. Не знает о Kafka:
// получает уже разобранные сообщения, ретраи и коммит offset'ов делает
// kafka.Consumer.
type Seeder struct {
	friends FollowerSource
	cache   FanoutCache
}

func NewSeeder(friends FollowerSource, cache FanoutCache) *Seeder {
	return &Seeder{friends: friends, cache: cache}
}

// Seed группирует посты по получателям и обновляет их ленты.
// Списки друзей всех авторов батча запрашиваются одним запросом к БД.
// Ошибка (БД или Redis) прерывает обработку батча целиком: консьюмер
// повторит его, а offset не закоммитит, пока запись не подтвердится.
func (s *Seeder) Seed(ctx context.Context, posts []domain.PublishedPost) error {
	if len(posts) == 0 {
		return nil
	}

	started := time.Now()

	authorIDs := distinctAuthors(posts)
	friendsByAuthor, err := s.friends.Followers(ctx, authorIDs)
	if err != nil {
		return fmt.Errorf("load followers: %w", err)
	}

	byRecipient := make(map[string][]rediscache.FanoutItem)
	for _, post := range posts {
		for _, friend := range friendsByAuthor[post.AuthorID] {
			byRecipient[friend] = append(byRecipient[friend], rediscache.FanoutItem{
				PostID: post.PostID,
				Score:  post.CreatedAtUnixMicro,
			})
		}
	}

	for recipient, items := range byRecipient {
		if err := s.cache.Fanout(ctx, recipient, items); err != nil {
			return fmt.Errorf("fanout feed %s: %w", recipient, err)
		}
	}

	metrics.FanoutRecipients.Add(float64(len(byRecipient)))
	metrics.FanoutDuration.Observe(time.Since(started).Seconds())

	return nil
}

// distinctAuthors возвращает авторов батча без повторов в порядке появления.
func distinctAuthors(posts []domain.PublishedPost) []string {
	seen := make(map[string]struct{}, len(posts))
	authorIDs := make([]string, 0, len(posts))

	for _, post := range posts {
		if _, ok := seen[post.AuthorID]; ok {
			continue
		}
		seen[post.AuthorID] = struct{}{}
		authorIDs = append(authorIDs, post.AuthorID)
	}

	return authorIDs
}

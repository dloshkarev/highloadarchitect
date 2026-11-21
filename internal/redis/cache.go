package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/dloshkarev/highloadarchitect/internal/config"
	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

// tombstoneValue — значение ключа post:{id} для удалённого поста.
const tombstoneValue = "__deleted__"

var (
	errUnexpectedPostValue = errors.New("unexpected post value type")

	updatePostScript = goredis.NewScript(`
local value = redis.call('GET', KEYS[1])
if value == '__deleted__' then
    return 0
end
redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2])
return 1
`)

	fanoutScript = goredis.NewScript(`
local unpack = unpack or table.unpack
if redis.call('EXISTS', KEYS[1]) == 0 then
    return 0
end
redis.call('ZADD', KEYS[1], unpack(ARGV, 2))
redis.call('ZREMRANGEBYRANK', KEYS[1], 0, -(tonumber(ARGV[1]) + 1))
return 1
`)
)

// FanoutItem — один пост для добавления в ленту получателя.
// Score — created_at поста в Unix-микросекундах.
type FanoutItem struct {
	PostID string
	Score  int64
}

// Cache инкапсулирует операции с Redis, относящиеся к лентам и телам постов.
type Cache struct {
	rdb *goredis.Client
	cfg config.FeedConfig
}

// FeedKey возвращает ключ Redis Sorted Set с идентификаторами постов ленты.
func FeedKey(userID string) string {
	return "feed:" + userID
}

// PostKey возвращает ключ Redis с JSON-телом поста.
func PostKey(postID string) string {
	return "post:" + postID
}

// Open создаёт Redis-клиент, проверяет соединение и возвращает кэш.
// При недоступном Redis приложение должно упасть на старте (fail fast).
func Open(ctx context.Context, redisCfg config.RedisConfig, feedCfg config.FeedConfig) (*Cache, error) {
	rdb := goredis.NewClient(&goredis.Options{
		Addr:         redisCfg.Addr,
		Password:     redisCfg.Password,
		DB:           redisCfg.DB,
		DialTimeout:  redisCfg.DialTimeout,
		ReadTimeout:  redisCfg.ReadTimeout,
		WriteTimeout: redisCfg.WriteTimeout,
		PoolSize:     redisCfg.PoolSize,
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()

		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return NewCache(rdb, feedCfg), nil
}

// NewCache создаёт кэш поверх готового клиента.
func NewCache(rdb *goredis.Client, cfg config.FeedConfig) *Cache {
	return &Cache{rdb: rdb, cfg: cfg}
}

// Close закрывает соединение с Redis.
func (c *Cache) Close() error {
	if err := c.rdb.Close(); err != nil {
		return fmt.Errorf("close redis: %w", err)
	}

	return nil
}

// Ping проверяет доступность Redis.
func (c *Cache) Ping(ctx context.Context) error {
	if err := c.rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}

	return nil
}

// Card возвращает количество постов в кэше ленты.
func (c *Cache) Card(ctx context.Context, userID string) (int64, error) {
	card, err := c.rdb.ZCard(ctx, FeedKey(userID)).Result()
	if err != nil {
		return 0, fmt.Errorf("zcard feed: %w", err)
	}

	return card, nil
}

// Range возвращает идентификаторы постов ленты [offset, offset+limit) от новых к старым.
func (c *Cache) Range(ctx context.Context, userID string, offset int, limit int) ([]string, error) {
	if offset < 0 || limit <= 0 {
		return nil, nil
	}

	ids, err := c.rdb.ZRangeArgs(ctx, goredis.ZRangeArgs{
		Key:   FeedKey(userID),
		Start: int64(offset),
		Stop:  int64(offset + limit - 1),
		Rev:   true,
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("zrevrange feed: %w", err)
	}

	return ids, nil
}

// Warm наполняет кэш ленты постами и выставляет TTL.
// Пустой список ничего не создаёт: Redis не хранит пустые sorted set.
func (c *Cache) Warm(ctx context.Context, userID string, posts []domain.Post) error {
	if len(posts) == 0 {
		return nil
	}

	key := FeedKey(userID)
	members := make([]goredis.Z, 0, len(posts))
	for _, post := range posts {
		members = append(members, goredis.Z{
			Score:  float64(post.CreatedAt.UnixMicro()),
			Member: post.ID,
		})
	}

	pipe := c.rdb.Pipeline()
	pipe.ZAdd(ctx, key, members...)
	pipe.Expire(ctx, key, c.cfg.TTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("warm feed: %w", err)
	}

	return nil
}

// Touch продлевает TTL ленты. Отсутствующий ключ не пересоздаётся.
func (c *Cache) Touch(ctx context.Context, userID string) error {
	if err := c.rdb.Expire(ctx, FeedKey(userID), c.cfg.TTL).Err(); err != nil {
		return fmt.Errorf("expire feed: %w", err)
	}

	return nil
}

// GetPosts читает тела постов по их идентификаторам.
// Возвращает найденные тела, tombstone-ключи и идентификаторы без ключа.
func (c *Cache) GetPosts(
	ctx context.Context,
	ids []string,
) (map[string]domain.Post, map[string]struct{}, []string, error) {
	bodies := make(map[string]domain.Post, len(ids))
	deleted := make(map[string]struct{})
	missing := make([]string, 0)
	if len(ids) == 0 {
		return bodies, deleted, missing, nil
	}

	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = PostKey(id)
	}

	values, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("mget posts: %w", err)
	}

	for i, value := range values {
		if value == nil {
			missing = append(missing, ids[i])

			continue
		}

		raw, ok := value.(string)
		if !ok {
			return nil, nil, nil, fmt.Errorf("%w: %T", errUnexpectedPostValue, value)
		}
		if raw == tombstoneValue {
			deleted[ids[i]] = struct{}{}

			continue
		}

		var post cachedPost
		if err := json.Unmarshal([]byte(raw), &post); err != nil {
			return nil, nil, nil, fmt.Errorf("decode post %s: %w", ids[i], err)
		}
		bodies[ids[i]] = post.toDomain()
	}

	return bodies, deleted, missing, nil
}

// PutPostIfAbsent кладёт тело поста, только если ключа ещё нет.
// NX не даёт перетереть tombstone или более свежее тело после edit.
func (c *Cache) PutPostIfAbsent(ctx context.Context, post domain.Post) error {
	body, err := json.Marshal(newCachedPost(post))
	if err != nil {
		return fmt.Errorf("encode post %s: %w", post.ID, err)
	}

	err = c.rdb.SetArgs(ctx, PostKey(post.ID), body, goredis.SetArgs{
		Mode: "NX",
		TTL:  c.cfg.PostTTL,
	}).Err()
	if err != nil && !errors.Is(err, goredis.Nil) {
		return fmt.Errorf("set post %s: %w", post.ID, err)
	}

	return nil
}

// UpdatePostIfNotDeleted обновляет тело поста, если он не помечен tombstone.
// Проверка и запись выполняются атомарно в Lua.
func (c *Cache) UpdatePostIfNotDeleted(ctx context.Context, post domain.Post) error {
	body, err := json.Marshal(newCachedPost(post))
	if err != nil {
		return fmt.Errorf("encode post %s: %w", post.ID, err)
	}

	err = updatePostScript.Run(ctx, c.rdb, []string{PostKey(post.ID)}, body, ttlSeconds(c.cfg.PostTTL)).Err()
	if err != nil {
		return fmt.Errorf("update post %s: %w", post.ID, err)
	}

	return nil
}

// Tombstone помечает пост удалённым на время TTL.
func (c *Cache) Tombstone(ctx context.Context, postID string) error {
	if err := c.rdb.Set(ctx, PostKey(postID), tombstoneValue, c.cfg.PostTTL).Err(); err != nil {
		return fmt.Errorf("tombstone post %s: %w", postID, err)
	}

	return nil
}

// Fanout добавляет посты в ленту получателя, только если лента уже существует.
// Ключ неактивного пользователя не создаётся. Размер ленты обрезается до CacheLimit.
func (c *Cache) Fanout(ctx context.Context, userID string, items []FanoutItem) error {
	if len(items) == 0 {
		return nil
	}

	args := make([]any, 0, 1+2*len(items))
	args = append(args, c.cfg.CacheLimit)
	for _, item := range items {
		args = append(args, item.Score, item.PostID)
	}

	if err := fanoutScript.Run(ctx, c.rdb, []string{FeedKey(userID)}, args...).Err(); err != nil {
		return fmt.Errorf("fanout feed %s: %w", userID, err)
	}

	return nil
}

func ttlSeconds(ttl time.Duration) int64 {
	seconds := int64(ttl / time.Second)
	if seconds <= 0 {
		return 1
	}

	return seconds
}

//go:build integration

package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
	rediscache "github.com/dloshkarev/highloadarchitect/internal/redis"
)

// Сценарии шага 11: полный путь new_post → outbox → Kafka → fanout → кэш
// ленты. Тесты стартуют на приложении из TestMain (см. app_integration_test.go)
// с migrate_seed: false и feed.enabled: true, cache_limit: 5, ttl: 1m.

const tombstoneValue = "__deleted__"

// feedUser — зарегистрированный пользователь с токеном.
type feedUser struct {
	id    string
	token string
}

// postView — пост в ответе ленты.
type postView struct {
	ID           string `json:"id"`
	Text         string `json:"text"`
	AuthorUserID string `json:"author_user_id"`
}

// newFeedUser регистрирует пользователя с уникальным именем и возвращает его с токеном.
func newFeedUser(t *testing.T, role string) feedUser {
	t.Helper()

	mark := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	user := namedProfile(role+"-"+mark, "Тестов-"+mark)
	id := registerUser(t, user)

	return feedUser{id: id, token: login(t, id, user.Password)}
}

// newFeedScenario создаёт автора и читателя, добавившего автора в друзья.
// В friends строка (user_id=читатель, friend_id=автор): читатель получает
// посты автора в ленту.
func newFeedScenario(t *testing.T) (author feedUser, reader feedUser) {
	t.Helper()

	author = newFeedUser(t, "Автор")
	reader = newFeedUser(t, "Читатель")
	addFriend(t, reader.token, author.id)

	return author, reader
}

func authedJSON(t *testing.T, method, path, token string, body any) (int, []byte) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, baseURL+path, reader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, data
}

func addFriend(t *testing.T, token, userID string) {
	t.Helper()

	status, body := authedJSON(t, http.MethodPut, "/friend/set/"+userID, token, nil)
	require.Equal(t, http.StatusOK, status, string(body))
}

func createPost(t *testing.T, token, text string) string {
	t.Helper()

	status, body := authedJSON(t, http.MethodPost, "/post/create", token, map[string]string{"text": text})
	require.Equal(t, http.StatusOK, status, string(body))

	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.NoError(t, uuid.Validate(created.ID))

	return created.ID
}

func getFeed(t *testing.T, token string, offset, limit int) []postView {
	t.Helper()

	status, body := authedJSON(
		t, http.MethodGet,
		fmt.Sprintf("/post/feed?offset=%d&limit=%d", offset, limit),
		token, nil,
	)
	require.Equal(t, http.StatusOK, status, string(body))

	return decodePosts(t, body)
}

func decodePosts(t *testing.T, body []byte) []postView {
	t.Helper()

	var posts []postView
	require.NoError(t, json.Unmarshal(body, &posts))

	return posts
}

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	t.Cleanup(func() { _ = rdb.Close() })

	return rdb
}

func queryScalar(t *testing.T, query string, args ...any) (string, bool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, postgresDSN)
	require.NoError(t, err)
	defer conn.Close(ctx)

	var value string
	err = conn.QueryRow(ctx, query, args...).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false
	}
	require.NoError(t, err)

	return value, true
}

func execSQL(t *testing.T, query string, args ...any) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, postgresDSN)
	require.NoError(t, err)
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, query, args...)
	require.NoError(t, err)
}

// waitCond опрашивает условие до успеха или истечения таймаута.
func waitCond(t *testing.T, timeout time.Duration, message string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal(message)
}

// kafkaPeekHandler пересылает в канал идентификатор искомого поста,
// встреченный в топике.
type kafkaPeekHandler struct {
	target   string
	received chan<- string
}

func (h *kafkaPeekHandler) Setup(sarama.ConsumerGroupSession) error { return nil }

func (h *kafkaPeekHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *kafkaPeekHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		var post domain.PublishedPost
		if err := json.Unmarshal(msg.Value, &post); err == nil && post.PostID == h.target {
			select {
			case h.received <- post.PostID:
			default:
			}
		}
		session.MarkMessage(msg, "")
	}

	return nil
}

// waitForKafkaMessage проверяет, что сообщение о конкретном посте опубликовано
// в Kafka: отдельная consumer-группа с новым идентификатором читает топик
// с начала, не пересекаясь с офсетами приложения.
func waitForKafkaMessage(t *testing.T, postID string) {
	t.Helper()

	cfg := sarama.NewConfig()
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	group, err := sarama.NewConsumerGroup(kafkaBrokers, "test-verify-"+postID, cfg)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, group.Close())
	}()

	received := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for ctx.Err() == nil {
			if err := group.Consume(ctx, []string{kafkaTopic}, &kafkaPeekHandler{target: postID, received: received}); err != nil {
				return
			}
		}
	}()

	select {
	case id := <-received:
		require.Equal(t, postID, id)
	case <-time.After(20 * time.Second):
		t.Fatal("kafka message was not published in time")
	}
}

// publishPostMessage публикует сообщение о посте в Kafka напрямую,
// имитируя повторную доставку (at-least-once).
func publishPostMessage(t *testing.T, post domain.PublishedPost) {
	t.Helper()

	cfg := sarama.NewConfig()
	cfg.Producer.Return.Successes = true
	producer, err := sarama.NewSyncProducer(kafkaBrokers, cfg)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, producer.Close())
	}()

	value, err := json.Marshal(post)
	require.NoError(t, err)

	_, _, err = producer.SendMessage(&sarama.ProducerMessage{
		Topic: kafkaTopic,
		Key:   sarama.StringEncoder(post.AuthorID),
		Value: sarama.ByteEncoder(value),
	})
	require.NoError(t, err)
}

// TestMetricsEndpoint проверяет экспорт метрик Prometheus.
// Ожидает 200 на /metrics и наличие счётчиков кэша ленты среди метрик.
func TestMetricsEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	status, body := doJSON(t, http.MethodGet, "/metrics", nil)
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, string(body), "feed_cache_hit_total")
	require.Contains(t, string(body), "feed_cache_miss_total")
	require.Contains(t, string(body), "outbox_lag")
	require.Contains(t, string(body), "fanout_recipients_total")
}

// TestCreatePostWritesPostAndOutbox проверяет, что создание поста через API
// пишет posts и outbox в одной транзакции.
// Ожидает строку в posts с текстом поста и событие new_post в outbox с этим post_id.
func TestCreatePostWritesPostAndOutbox(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	author := newFeedUser(t, "Автор")

	postID := createPost(t, author.token, "текст первого поста")

	body, ok := queryScalar(t, `SELECT body FROM posts WHERE id = $1`, postID)
	require.True(t, ok, "post row not found")
	require.Equal(t, "текст первого поста", body)

	eventType, ok := queryScalar(t,
		`SELECT event_type FROM outbox WHERE payload->>'post_id' = $1 AND event_type = $2`,
		postID, domain.OutboxEventNewPost,
	)
	require.True(t, ok, "outbox event not found")
	require.Equal(t, domain.OutboxEventNewPost, eventType)
}

// TestOutboxWorkerPublishesToKafkaAndCachesPost проверяет обработку new_post
// outbox-воркером.
// Ожидает появление тела поста в Redis post:{id} и сообщение в Kafka.
func TestOutboxWorkerPublishesToKafkaAndCachesPost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	author := newFeedUser(t, "Автор")
	postID := createPost(t, author.token, "пост для kafka")

	rdb := newTestRedis(t)
	waitCond(t, 15*time.Second, "post body was not cached", func() bool {
		return rdb.Exists(context.Background(), rediscache.PostKey(postID)).Val() == 1
	})

	waitForKafkaMessage(t, postID)
}

// TestFanoutAddsPostToActiveFriendsOnly проверяет fanout консьюмера.
// Ожидает: пост появляется в feed:{id} активного друга (который читал ленту),
// у неактивного друга ключ feed:{id} не создаётся.
func TestFanoutAddsPostToActiveFriendsOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	author, reader := newFeedScenario(t)
	inactive := newFeedUser(t, "Спящий")
	addFriend(t, inactive.token, author.id)

	// Первый пост попадает в ленту читателя через прогрев из БД:
	// читаем ленту, чтобы ключ feed:{id} появился.
	first := createPost(t, author.token, "пост до прогрева")
	feed := getFeed(t, reader.token, 0, 10)
	require.Len(t, feed, 1)
	require.Equal(t, first, feed[0].ID)

	rdb := newTestRedis(t)
	require.Equal(t, int64(1), rdb.Exists(ctx, rediscache.FeedKey(reader.id)).Val())

	// Второй пост раскладывается консьюмером в уже существующий ключ.
	second := createPost(t, author.token, "пост после прогрева")
	waitCond(t, 15*time.Second, "fanout did not add post to active friend", func() bool {
		return rdb.ZScore(ctx, rediscache.FeedKey(reader.id), second).Err() == nil
	})

	require.Equal(t, int64(0), rdb.Exists(ctx, rediscache.FeedKey(inactive.id)).Val(),
		"inactive friend should not get feed key")
}

// TestFeedServesFromCacheWithoutDB проверяет, что чтение ленты в пределах
// кэша не ходит в БД.
// Ожидает: прогретый пост возвращается из ленты даже после удаления строки
// из posts напрямую в базе.
func TestFeedServesFromCacheWithoutDB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	author, reader := newFeedScenario(t)

	postID := createPost(t, author.token, "пост в кэше")

	// limit=1 при одном посте: 0+1 <= ZCARD — чтение идёт по кэш-пути.
	feed := getFeed(t, reader.token, 0, 1)
	require.Len(t, feed, 1)
	require.Equal(t, postID, feed[0].ID)
	require.Equal(t, "пост в кэше", feed[0].Text)

	execSQL(t, `DELETE FROM posts WHERE id = $1`, postID)

	feed = getFeed(t, reader.token, 0, 1)
	require.Len(t, feed, 1)
	require.Equal(t, postID, feed[0].ID)
	require.Equal(t, "пост в кэше", feed[0].Text)
}

// TestFeedBeyondCacheReadsFromDB проверяет fallback в БД за границей кэша.
// Ожидает: при cache_limit=5 и семи постах первая страница отдаёт 5 свежих
// из кэша, вторая страница (offset=5) — два старых поста из БД.
func TestFeedBeyondCacheReadsFromDB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	author, reader := newFeedScenario(t)

	ids := make([]string, 0, 7)
	for i := 1; i <= 7; i++ {
		ids = append(ids, createPost(t, author.token, fmt.Sprintf("пост %d", i)))
	}

	first := getFeed(t, reader.token, 0, 5)
	require.Len(t, first, 5)
	require.Equal(t, ids[6], first[0].ID, "newest post should be first")

	second := getFeed(t, reader.token, 5, 3)
	require.Len(t, second, 2)
	require.Equal(t, ids[1], second[0].ID)
	require.Equal(t, ids[0], second[1].ID)
}

// TestPostBodyCacheMissFillsFromDB проверяет восстановление тела поста в кэше.
// Ожидает: после удаления ключа post:{id} чтение ленты возвращает пост из БД
// и пересоздаёт ключ с ненулевым TTL.
func TestPostBodyCacheMissFillsFromDB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	author, reader := newFeedScenario(t)
	postID := createPost(t, author.token, "пост для промаха")

	// limit=1 при одном посте: 0+1 <= ZCARD — тело поста попадёт в кэш.
	feed := getFeed(t, reader.token, 0, 1)
	require.Len(t, feed, 1)

	rdb := newTestRedis(t)
	require.Equal(t, int64(1), rdb.Del(context.Background(), rediscache.PostKey(postID)).Val())

	feed = getFeed(t, reader.token, 0, 1)
	require.Len(t, feed, 1)
	require.Equal(t, "пост для промаха", feed[0].Text)

	ttl := rdb.TTL(context.Background(), rediscache.PostKey(postID)).Val()
	require.Greater(t, ttl, time.Duration(0), "post key was not recreated")
}

// TestEditUpdatesCachedBody проверяет обновление тела поста в кэше.
// Ожидает: после PUT /post/update и ключ post:{id}, и лента содержат новый текст.
func TestEditUpdatesCachedBody(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	author, reader := newFeedScenario(t)
	postID := createPost(t, author.token, "старый текст")

	// limit=1 при одном посте: 0+1 <= ZCARD — чтение идёт по кэш-пути.
	feed := getFeed(t, reader.token, 0, 1)
	require.Len(t, feed, 1)
	require.Equal(t, "старый текст", feed[0].Text)

	status, body := authedJSON(t, http.MethodPut, "/post/update", author.token,
		map[string]string{"id": postID, "text": "новый текст"})
	require.Equal(t, http.StatusOK, status, string(body))

	rdb := newTestRedis(t)
	waitCond(t, 15*time.Second, "post body was not updated in cache", func() bool {
		value, err := rdb.Get(context.Background(), rediscache.PostKey(postID)).Result()
		if err != nil {
			return false
		}
		var cached struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(value), &cached) != nil {
			return false
		}

		return cached.Text == "новый текст"
	})

	feed = getFeed(t, reader.token, 0, 1)
	require.Len(t, feed, 1)
	require.Equal(t, "новый текст", feed[0].Text)
}

// TestDeleteTombstonesPostAndFeedSkipsIt проверяет удаление поста.
// Ожидает: post:{id} становится tombstone, лента перестаёт отдавать пост.
func TestDeleteTombstonesPostAndFeedSkipsIt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	author, reader := newFeedScenario(t)
	postID := createPost(t, author.token, "пост на удаление")

	// limit=1 при одном посте: 0+1 <= ZCARD — чтение идёт по кэш-пути.
	feed := getFeed(t, reader.token, 0, 1)
	require.Len(t, feed, 1)

	status, body := authedJSON(t, http.MethodPut, "/post/delete/"+postID, author.token, nil)
	require.Equal(t, http.StatusOK, status, string(body))

	rdb := newTestRedis(t)
	waitCond(t, 15*time.Second, "tombstone was not written", func() bool {
		value, err := rdb.Get(context.Background(), rediscache.PostKey(postID)).Result()
		return err == nil && value == tombstoneValue
	})

	feed = getFeed(t, reader.token, 0, 1)
	require.Empty(t, feed)
}

// TestEditAfterDeleteKeepsTombstone проверяет, что удалённый пост нельзя
// отредактировать и tombstone не перетирается.
// Ожидает 404 на обновление удалённого поста и tombstone в post:{id}.
func TestEditAfterDeleteKeepsTombstone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	author := newFeedUser(t, "Автор")
	postID := createPost(t, author.token, "удалённый пост")

	status, body := authedJSON(t, http.MethodPut, "/post/delete/"+postID, author.token, nil)
	require.Equal(t, http.StatusOK, status, string(body))

	rdb := newTestRedis(t)
	waitCond(t, 15*time.Second, "tombstone was not written", func() bool {
		value, err := rdb.Get(context.Background(), rediscache.PostKey(postID)).Result()
		return err == nil && value == tombstoneValue
	})

	status, body = authedJSON(t, http.MethodPut, "/post/update", author.token,
		map[string]string{"id": postID, "text": "попытка правки"})
	require.Equal(t, http.StatusNotFound, status, string(body))

	value, err := rdb.Get(context.Background(), rediscache.PostKey(postID)).Result()
	require.NoError(t, err)
	require.Equal(t, tombstoneValue, value)
}

// TestDuplicateKafkaDeliveryDoesNotDoubleFeed проверяет идемпотентность fanout.
// Ожидает: повторная доставка того же сообщения Kafka не создаёт дублей в
// ZSET и не меняет score поста.
func TestDuplicateKafkaDeliveryDoesNotDoubleFeed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	author, reader := newFeedScenario(t)
	postID := createPost(t, author.token, "пост для дубля")

	feed := getFeed(t, reader.token, 0, 10)
	require.Len(t, feed, 1)

	rdb := newTestRedis(t)
	score := rdb.ZScore(ctx, rediscache.FeedKey(reader.id), postID).Val()

	// Повторная доставка: то же сообщение с тем же post_id и score.
	publishPostMessage(t, domain.PublishedPost{
		PostID:             postID,
		AuthorID:           author.id,
		CreatedAtUnixMicro: int64(score),
	})
	time.Sleep(3 * time.Second)

	require.Equal(t, int64(1), rdb.ZCard(ctx, rediscache.FeedKey(reader.id)).Val(),
		"duplicate delivery created a second member in feed")
	require.Equal(t, score, rdb.ZScore(ctx, rediscache.FeedKey(reader.id), postID).Val(),
		"duplicate delivery changed the post score")
}

// TestFeedTTLProlongedOnRead проверяет продление TTL ленты на чтении.
// Ожидает: после паузы TTL уменьшается, а повторное чтение возвращает его
// к значению конфигурации.
func TestFeedTTLProlongedOnRead(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	author, reader := newFeedScenario(t)
	createPost(t, author.token, "пост для ttl")

	rdb := newTestRedis(t)
	feed := getFeed(t, reader.token, 0, 10)
	require.Len(t, feed, 1)

	ttlAfterFirst := rdb.TTL(context.Background(), rediscache.FeedKey(reader.id)).Val()
	require.Greater(t, ttlAfterFirst, 50*time.Second, "feed TTL is too short after first read")

	time.Sleep(3 * time.Second)
	ttlBefore := rdb.TTL(context.Background(), rediscache.FeedKey(reader.id)).Val()
	require.Less(t, ttlBefore, ttlAfterFirst-2*time.Second, "TTL should decrease without reads")

	getFeed(t, reader.token, 0, 10)

	ttlAfterSecond := rdb.TTL(context.Background(), rediscache.FeedKey(reader.id)).Val()
	require.Greater(t, ttlAfterSecond, ttlBefore+2*time.Second, "read should prolong feed TTL")
}

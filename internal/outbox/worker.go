package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/avast/retry-go/v4"

	"github.com/dloshkarev/highloadarchitect/internal/config"
	"github.com/dloshkarev/highloadarchitect/internal/domain"
	"github.com/dloshkarev/highloadarchitect/internal/repository"
)

const (
	retryDelay    = 100 * time.Millisecond
	retryMaxDelay = 2 * time.Second
)

var (
	errUnknownEventType      = errors.New("unknown outbox event type")
	errProducerNotConfigured = errors.New("outbox producer is not configured")
)

// Producer публикует события о новых постах во внешнюю шину.
type Producer interface {
	Publish(ctx context.Context, post domain.PublishedPost) error
}

// Cache — операции с кэшем тел постов, нужные outbox-воркеру.
type Cache interface {
	PutPostIfAbsent(ctx context.Context, post domain.Post) error
	UpdatePostIfNotDeleted(ctx context.Context, post domain.Post) error
	Tombstone(ctx context.Context, postID string) error
}

// Worker читает события outbox и применяет их к Kafka и кэшу постов.
type Worker struct {
	repo     repository.OutboxRepository
	producer Producer
	cache    Cache
	cfg      config.OutboxConfig

	mu sync.Mutex
}

func NewWorker(
	repo repository.OutboxRepository,
	producer Producer,
	cache Cache,
	cfg config.OutboxConfig,
) *Worker {
	return &Worker{repo: repo, producer: producer, cache: cache, cfg: cfg}
}

// Run обрабатывает outbox по тикеру до отмены контекста.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.process(ctx)
		}
	}
}

// process забирает один батч и обрабатывает события независимо друг от друга.
// Тики не пересекаются: если предыдущая обработка ещё идёт, тик пропускается.
func (w *Worker) process(ctx context.Context) {
	if !w.mu.TryLock() {
		slog.Warn("outbox worker is busy, skipping tick")

		return
	}
	defer w.mu.Unlock()

	events, err := w.repo.GetBatch(ctx, w.cfg.BatchSize)
	if err != nil {
		slog.Error("outbox batch read failed", "err", err)

		return
	}

	for _, event := range events {
		if ctx.Err() != nil {
			return
		}

		if handleErr := w.handle(ctx, event); handleErr != nil {
			slog.Error("outbox event failed",
				"event_id", event.ID,
				"event_type", event.Type,
				"payload", string(event.Payload),
				"err", handleErr,
			)
		}
		if deleteErr := w.repo.Delete(ctx, event.ID); deleteErr != nil {
			slog.Error("outbox event delete failed", "event_id", event.ID, "err", deleteErr)
		}
	}
}

func (w *Worker) handle(ctx context.Context, event domain.OutboxEvent) error {
	switch event.Type {
	case domain.OutboxEventNewPost:
		return w.handleNewPost(ctx, event.Payload)
	case domain.OutboxEventEditPost:
		return w.handleEditPost(ctx, event.Payload)
	case domain.OutboxEventDeletePost:
		return w.handleDeletePost(ctx, event.Payload)
	default:
		return fmt.Errorf("%w: %q", errUnknownEventType, event.Type)
	}
}

// handleNewPost сначала публикует событие в Kafka, затем кладёт тело поста
// в кэш: потеря кэша восстановима из БД, потеря Kafka-сообщения — нет.
func (w *Worker) handleNewPost(ctx context.Context, payload []byte) error {
	var event domain.NewPostPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("decode new_post payload: %w", err)
	}
	if w.producer == nil {
		return errProducerNotConfigured
	}

	post := domain.Post{
		ID:        event.PostID,
		AuthorID:  event.AuthorID,
		Text:      event.Body,
		CreatedAt: time.UnixMicro(event.CreatedAtUnixMicro).UTC(),
	}

	if err := w.producer.Publish(ctx, domain.PublishedPost{
		PostID:             post.ID,
		AuthorID:           post.AuthorID,
		CreatedAtUnixMicro: event.CreatedAtUnixMicro,
	}); err != nil {
		return fmt.Errorf("publish new post: %w", err)
	}

	return w.retryCache(ctx, "put post", func() error {
		return w.cache.PutPostIfAbsent(ctx, post)
	})
}

func (w *Worker) handleEditPost(ctx context.Context, payload []byte) error {
	var event domain.EditPostPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("decode edit_post payload: %w", err)
	}

	return w.retryCache(ctx, "update post", func() error {
		return w.cache.UpdatePostIfNotDeleted(ctx, domain.Post{
			ID:       event.PostID,
			AuthorID: event.AuthorID,
			Text:     event.Body,
		})
	})
}

func (w *Worker) handleDeletePost(ctx context.Context, payload []byte) error {
	var event domain.DeletePostPayload
	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("decode delete_post payload: %w", err)
	}

	return w.retryCache(ctx, "tombstone post", func() error {
		return w.cache.Tombstone(ctx, event.PostID)
	})
}

// retryCache повторяет запись в Redis бесконечно до успеха или отмены контекста.
func (w *Worker) retryCache(ctx context.Context, operation string, fn func() error) error {
	err := retry.Do(
		fn,
		retry.Attempts(0),
		retry.Delay(retryDelay),
		retry.MaxDelay(retryMaxDelay),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
	)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}

	return nil
}

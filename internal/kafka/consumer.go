package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/IBM/sarama"
	"github.com/avast/retry-go/v4"

	"github.com/dloshkarev/highloadarchitect/internal/config"
	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

const (
	// reconnectDelay — пауза перед переподключением группы после ошибки.
	reconnectDelay = time.Second
	// retryDelay и retryMaxDelay — границы backoff для ретраев обработки и коммита.
	retryDelay    = 100 * time.Millisecond
	retryMaxDelay = 2 * time.Second
)

// saramaEnvelope связывает исходное сообщение sarama с сессией, в которой оно
// было получено: коммитить offset нужно сессией-владельцем партиции, коммит
// чужой или устаревшей сессии привёл бы к пропуску необработанных сообщений.
type saramaEnvelope struct {
	msg     *sarama.ConsumerMessage
	session sarama.ConsumerGroupSession
}

// groupHandler пересылает сообщения claim'ов в канал консьюмера.
type groupHandler struct {
	consumer *Consumer
}

// BatchProcessor обрабатывает разобранный батч постов. Реализуется пакетом
// internal/service/feed; пакеты не импортируют друг друга.
type BatchProcessor interface {
	Seed(ctx context.Context, posts []domain.PublishedPost) error
}

// Consumer читает батчи сообщений из Kafka, разбирает их и передаёт процессору.
// Здесь же живёт вся механика Kafka: сборка батча, парсинг, коммит offset'ов,
// переподключение группы. Offset коммитится только после успешной обработки.
type Consumer struct {
	group     sarama.ConsumerGroup
	topic     string
	processor BatchProcessor
	cfg       config.KafkaConfig
	msgs      chan saramaEnvelope
	cancel    context.CancelFunc
	done      chan struct{}
}

func NewConsumer(cfg config.KafkaConfig, processor BatchProcessor) (*Consumer, error) {
	brokers, err := cfg.BrokersList()
	if err != nil {
		return nil, fmt.Errorf("parse kafka brokers: %w", err)
	}

	group, err := sarama.NewConsumerGroup(brokers, cfg.GroupID, consumerGroupConfig())
	if err != nil {
		return nil, fmt.Errorf("create kafka consumer group: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &Consumer{
		group:     group,
		topic:     cfg.Topic,
		processor: processor,
		cfg:       cfg,
		msgs:      make(chan saramaEnvelope, cfg.BatchSize),
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	go c.consumeLoop(ctx)

	return c, nil
}

// Run читает и обрабатывает батчи до отмены контекста.
func (c *Consumer) Run(ctx context.Context) {
	for ctx.Err() == nil {
		envelopes, err := c.fetchBatch(ctx)
		if err != nil {
			// context.Canceled приходит и при отмене родительского контекста,
			// и при остановленном консьюмере — в обоих случаях выходим.
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return
			}
			slog.Error("kafka fetch failed", "err", err)
			if !waitDelay(ctx, retryMaxDelay) {
				return
			}

			continue
		}
		if len(envelopes) == 0 {
			continue
		}

		posts := parseMessages(envelopes)
		if err := c.processWithRetry(ctx, posts); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("kafka batch processing stopped", "err", err, "posts", len(posts))

			return
		}
		if err := c.commitWithRetry(ctx, envelopes); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("kafka commit stopped", "err", err, "messages", len(envelopes))

			return
		}
	}
}

// Close останавливает цикл ConsumerGroup и закрывает группу.
func (c *Consumer) Close() error {
	c.cancel()
	err := c.group.Close()
	<-c.done

	if err != nil {
		return fmt.Errorf("close kafka consumer: %w", err)
	}

	return nil
}

// consumeLoop крутит цикл ConsumerGroup до отмены контекста, переподключаясь
// после ошибок. После ребалансировки сессия завершается штатно, и цикл начинает новую.
func (c *Consumer) consumeLoop(ctx context.Context) {
	defer close(c.done)

	for {
		err := c.group.Consume(ctx, []string{c.topic}, &groupHandler{consumer: c})
		if err != nil {
			if errors.Is(err, sarama.ErrClosedConsumerGroup) || ctx.Err() != nil {
				return
			}
			slog.Error("kafka consume failed", "err", err)
			if !waitDelay(ctx, reconnectDelay) {
				return
			}
		}

		if ctx.Err() != nil {
			return
		}
	}
}

// fetchBatch читает до batch_size сообщений или до batch_timeout.
// Если таймаут наступил после части сообщений, возвращается частичный батч.
func (c *Consumer) fetchBatch(ctx context.Context) ([]saramaEnvelope, error) {
	batchCtx, cancel := context.WithTimeout(ctx, c.cfg.BatchTimeout)
	defer cancel()

	envelopes := make([]saramaEnvelope, 0, c.cfg.BatchSize)
	for len(envelopes) < c.cfg.BatchSize {
		env, err := c.fetchMessage(batchCtx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return nil, fmt.Errorf("fetch kafka message: %w", err)
			}
			if len(envelopes) > 0 {
				return envelopes, nil
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, nil
			}

			return nil, fmt.Errorf("fetch kafka message: %w", err)
		}
		envelopes = append(envelopes, env)
	}

	return envelopes, nil
}

// fetchMessage возвращает одно сообщение группы. Блокируется до появления
// сообщения, отмены ctx или остановки консьюмера.
func (c *Consumer) fetchMessage(ctx context.Context) (saramaEnvelope, error) {
	select {
	case <-ctx.Done():
		return saramaEnvelope{}, fmt.Errorf("fetch kafka message: %w", ctx.Err())
	case <-c.done:
		return saramaEnvelope{}, fmt.Errorf("fetch kafka message: consumer is closed: %w", context.Canceled)
	case env := <-c.msgs:
		return env, nil
	}
}

// processWithRetry повторяет обработку батча до успеха или отмены контекста.
// Offset не коммитится, пока процессор не подтвердил запись лент.
func (c *Consumer) processWithRetry(ctx context.Context, posts []domain.PublishedPost) error {
	err := retry.Do(
		func() error {
			return c.processor.Seed(ctx, posts)
		},
		retry.Attempts(0),
		retry.Delay(retryDelay),
		retry.MaxDelay(retryMaxDelay),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.OnRetry(func(attempt uint, err error) {
			slog.Error("kafka batch processing failed", "attempt", attempt, "posts", len(posts), "err", err)
		}),
	)
	if err != nil {
		return fmt.Errorf("process kafka batch: %w", err)
	}

	return nil
}

func (c *Consumer) commitWithRetry(ctx context.Context, envelopes []saramaEnvelope) error {
	err := retry.Do(
		func() error {
			return c.commitMessages(ctx, envelopes)
		},
		retry.Attempts(0),
		retry.Delay(retryDelay),
		retry.MaxDelay(retryMaxDelay),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.Context(ctx),
		retry.OnRetry(func(attempt uint, err error) {
			slog.Error("kafka commit failed", "attempt", attempt, "messages", len(envelopes), "err", err)
		}),
	)
	if err != nil {
		return fmt.Errorf("commit kafka messages: %w", err)
	}

	return nil
}

// commitMessages коммитит offset'ы обработанных сообщений сессиями, в которых
// они были получены. После ребалансировки коммит устаревшей сессии sarama
// игнорирует, и сообщение будет доставлено повторно: семантика at-least-once.
func (c *Consumer) commitMessages(ctx context.Context, envelopes []saramaEnvelope) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("commit kafka messages: %w", err)
	}

	sessions := make(map[sarama.ConsumerGroupSession]struct{}, 1)
	for _, env := range envelopes {
		env.session.MarkMessage(env.msg, "")
		sessions[env.session] = struct{}{}
	}

	for session := range sessions {
		session.Commit()
	}

	return nil
}

// parseMessages разбирает значения сообщений в PublishedPost. Битые сообщения
// (невалидный JSON) логируются и пропускаются, чтобы не блокировать партицию:
// их offset всё равно коммитится вместе с батчем.
func parseMessages(envelopes []saramaEnvelope) []domain.PublishedPost {
	posts := make([]domain.PublishedPost, 0, len(envelopes))
	for _, env := range envelopes {
		var post domain.PublishedPost
		if err := json.Unmarshal(env.msg.Value, &post); err != nil {
			slog.Error("kafka message decode failed",
				"topic", env.msg.Topic,
				"partition", env.msg.Partition,
				"offset", env.msg.Offset,
				"err", err,
			)

			continue
		}
		posts = append(posts, post)
	}

	return posts
}

func (h *groupHandler) Setup(sarama.ConsumerGroupSession) error { return nil }

func (h *groupHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

// ConsumeClaim пересылает сообщения партиции в общий канал консьюмера.
// Отправка на переполненном канале разблокируется отменой сессии при shutdown.
func (h *groupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		select {
		case h.consumer.msgs <- saramaEnvelope{msg: msg, session: session}:
		case <-session.Context().Done():
			return nil
		}
	}

	return nil
}

func consumerGroupConfig() *sarama.Config {
	cfg := sarama.NewConfig()
	// Offset коммитится вручную из commitMessages после успешной обработки.
	cfg.Consumer.Offsets.AutoCommit.Enable = false
	// Новая группа читает топик с начала: события, созданные до первого
	// запуска консьюмера, не теряются. При перезапусках группа продолжает
	// с закоммиченных offset'ов, поэтому повторов всего топика не будет.
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest

	return cfg
}

func waitDelay(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

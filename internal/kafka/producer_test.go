package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/IBM/sarama"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

type fakeSyncProducer struct {
	messages []*sarama.ProducerMessage
	writeErr error
	closeErr error
	closed   bool
}

func (f *fakeSyncProducer) SendMessage(msg *sarama.ProducerMessage) (int32, int64, error) {
	if f.writeErr != nil {
		return 0, 0, f.writeErr
	}
	f.messages = append(f.messages, msg)

	return 0, 0, nil
}

func (f *fakeSyncProducer) Close() error {
	f.closed = true

	return f.closeErr
}

// TestProducerPublish проверяет публикацию поста в Kafka.
// Ожидает одно сообщение в заданный топик с ключом author_id и JSON PublishedPost в значении.
func TestProducerPublish(t *testing.T) {
	t.Parallel()

	writer := &fakeSyncProducer{}
	producer := newProducer(writer, "post-events")
	post := domain.PublishedPost{
		PostID:             "post-1",
		AuthorID:           "author-1",
		CreatedAtUnixMicro: 1767225600000000,
	}

	if err := producer.Publish(context.Background(), post); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if len(writer.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(writer.messages))
	}
	msg := writer.messages[0]
	if msg.Topic != "post-events" {
		t.Fatalf("topic = %q, want post-events", msg.Topic)
	}
	if got := string(msg.Key.(sarama.StringEncoder)); got != post.AuthorID {
		t.Fatalf("message key = %q, want %q", got, post.AuthorID)
	}

	var decoded domain.PublishedPost
	if err := json.Unmarshal([]byte(msg.Value.(sarama.ByteEncoder)), &decoded); err != nil {
		t.Fatalf("decode message value: %v", err)
	}
	if decoded != post {
		t.Fatalf("message value = %+v, want %+v", decoded, post)
	}
}

// TestProducerPublishError проверяет проброс ошибки записи в Kafka.
// Ожидает ошибку, совпадающую с ошибкой продюсера.
func TestProducerPublishError(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("kafka is down")
	producer := newProducer(&fakeSyncProducer{writeErr: writeErr}, "post-events")

	if err := producer.Publish(context.Background(), domain.PublishedPost{PostID: "post-1"}); !errors.Is(err, writeErr) {
		t.Fatalf("Publish() error = %v, want %v", err, writeErr)
	}
}

// TestProducerPublishContextCanceled проверяет проверку контекста перед отправкой.
// Ожидает ошибку context.Canceled и отсутствие вызова SendMessage.
func TestProducerPublishContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	writer := &fakeSyncProducer{}
	producer := newProducer(writer, "post-events")

	err := producer.Publish(ctx, domain.PublishedPost{PostID: "post-1"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Publish() error = %v, want context.Canceled", err)
	}
	if len(writer.messages) != 0 {
		t.Fatalf("messages = %d, want 0", len(writer.messages))
	}
}

// TestProducerClose проверяет закрытие продюсера через Close.
// Ожидает вызов producer.Close и nil-ошибку.
func TestProducerClose(t *testing.T) {
	t.Parallel()

	writer := &fakeSyncProducer{}
	producer := newProducer(writer, "post-events")

	if err := producer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !writer.closed {
		t.Fatal("producer was not closed")
	}
}

// TestProducerCloseError проверяет проброс ошибки закрытия продюсера.
// Ожидает ошибку, совпадающую с ошибкой Close.
func TestProducerCloseError(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("close failed")
	producer := newProducer(&fakeSyncProducer{closeErr: closeErr}, "post-events")

	if err := producer.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("Close() error = %v, want %v", err, closeErr)
	}
}

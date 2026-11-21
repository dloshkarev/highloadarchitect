package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/IBM/sarama"

	"github.com/dloshkarev/highloadarchitect/internal/config"
	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

func testKafkaConfig() config.KafkaConfig {
	return config.KafkaConfig{
		BatchSize:    10,
		BatchTimeout: 50 * time.Millisecond,
	}
}

func testEnvelope(t *testing.T, post domain.PublishedPost) saramaEnvelope {
	t.Helper()

	value, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshal post: %v", err)
	}

	return saramaEnvelope{
		msg: &sarama.ConsumerMessage{Topic: "post-events", Value: value},
	}
}

func testConsumer(processor BatchProcessor) *Consumer {
	return &Consumer{
		topic:     "post-events",
		processor: processor,
		cfg:       testKafkaConfig(),
		msgs:      make(chan saramaEnvelope, 16),
		done:      make(chan struct{}),
	}
}

type fakeProcessor struct {
	calls    [][]domain.PublishedPost
	failures int
}

func (f *fakeProcessor) Seed(_ context.Context, posts []domain.PublishedPost) error {
	if f.failures > 0 {
		f.failures--

		return errors.New("redis is down")
	}
	f.calls = append(f.calls, posts)

	return nil
}

// fakeGroupSession записывает вызовы MarkMessage и Commit.
type fakeGroupSession struct {
	marked  []*sarama.ConsumerMessage
	commits int
}

func (s *fakeGroupSession) Claims() map[string][]int32 { return nil }

func (s *fakeGroupSession) MemberID() string { return "member-1" }

func (s *fakeGroupSession) GenerationID() int32 { return 1 }

func (s *fakeGroupSession) MarkOffset(string, int32, int64, string) {}

func (s *fakeGroupSession) ResetOffset(string, int32, int64, string) {}

func (s *fakeGroupSession) MarkMessage(msg *sarama.ConsumerMessage, _ string) {
	s.marked = append(s.marked, msg)
}

func (s *fakeGroupSession) Context() context.Context { return context.Background() }

func (s *fakeGroupSession) Commit() { s.commits++ }

// TestFetchBatchStopsAtBatchSize проверяет ограничение размера батча.
// Ожидает 2 сообщения при batch_size=2 и трёх доступных сообщениях.
func TestFetchBatchStopsAtBatchSize(t *testing.T) {
	t.Parallel()

	consumer := testConsumer(&fakeProcessor{})
	for i := 0; i < 3; i++ {
		consumer.msgs <- saramaEnvelope{}
	}
	cfg := testKafkaConfig()
	cfg.BatchSize = 2
	consumer.cfg = cfg

	envelopes, err := consumer.fetchBatch(context.Background())
	if err != nil {
		t.Fatalf("fetchBatch() error = %v", err)
	}
	if len(envelopes) != 2 {
		t.Fatalf("envelopes = %d, want 2", len(envelopes))
	}
	if len(consumer.msgs) != 1 {
		t.Fatalf("remaining messages = %d, want 1", len(consumer.msgs))
	}
}

// TestFetchBatchReturnsPartialOnTimeout проверяет обработку частичного батча по таймауту.
// Ожидает 1 доступное сообщение без ошибки, хотя batch_size больше.
func TestFetchBatchReturnsPartialOnTimeout(t *testing.T) {
	t.Parallel()

	consumer := testConsumer(&fakeProcessor{})
	consumer.msgs <- saramaEnvelope{}

	envelopes, err := consumer.fetchBatch(context.Background())
	if err != nil {
		t.Fatalf("fetchBatch() error = %v", err)
	}
	if len(envelopes) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envelopes))
	}
}

// TestFetchBatchEmptyOnTimeout проверяет батч без сообщений при таймауте.
// Ожидает nil-ошибку и 0 сообщений.
func TestFetchBatchEmptyOnTimeout(t *testing.T) {
	t.Parallel()

	consumer := testConsumer(&fakeProcessor{})

	envelopes, err := consumer.fetchBatch(context.Background())
	if err != nil {
		t.Fatalf("fetchBatch() error = %v", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("envelopes = %d, want 0", len(envelopes))
	}
}

// TestFetchBatchStopsOnContextCancel проверяет остановку чтения по отменённому контексту.
// Ожидает ошибку, совместимую с context.Canceled.
func TestFetchBatchStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	consumer := testConsumer(&fakeProcessor{})

	if _, err := consumer.fetchBatch(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("fetchBatch() error = %v, want context.Canceled", err)
	}
}

// TestFetchBatchStopsOnClosedConsumer проверяет чтение после остановки консьюмера.
// Ожидает ошибку, совместимую с context.Canceled.
func TestFetchBatchStopsOnClosedConsumer(t *testing.T) {
	t.Parallel()

	consumer := testConsumer(&fakeProcessor{})
	close(consumer.done)

	if _, err := consumer.fetchBatch(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("fetchBatch() error = %v, want context.Canceled", err)
	}
}

// TestParseMessagesSkipsInvalid проверяет парсинг значений сообщений.
// Ожидает пропуск сообщения с невалидным JSON и разбор валидных.
func TestParseMessagesSkipsInvalid(t *testing.T) {
	t.Parallel()

	valid := domain.PublishedPost{PostID: "post-1", AuthorID: "author-1", CreatedAtUnixMicro: 100}
	envelopes := []saramaEnvelope{
		{msg: &sarama.ConsumerMessage{Topic: "post-events", Partition: 2, Offset: 7, Value: []byte("{")}},
		testEnvelope(t, valid),
	}

	posts := parseMessages(envelopes)
	if len(posts) != 1 || posts[0] != valid {
		t.Fatalf("posts = %v, want [%v]", posts, valid)
	}
}

// TestProcessWithRetryRetriesProcessor проверяет ретрай обработки при сбое процессора.
// Ожидает повторный вызов Seed после первой ошибки и успешную обработку постов.
func TestProcessWithRetryRetriesProcessor(t *testing.T) {
	t.Parallel()

	posts := []domain.PublishedPost{
		{PostID: "post-1", AuthorID: "author-1", CreatedAtUnixMicro: 100},
	}
	processor := &fakeProcessor{failures: 1}
	consumer := testConsumer(processor)

	if err := consumer.processWithRetry(context.Background(), posts); err != nil {
		t.Fatalf("processWithRetry() error = %v", err)
	}

	if len(processor.calls) != 1 {
		t.Fatalf("seed calls = %d, want 1", len(processor.calls))
	}
	if len(processor.calls[0]) != 1 || processor.calls[0][0] != posts[0] {
		t.Fatalf("seed posts = %v, want %v", processor.calls[0], posts)
	}
}

// TestProcessWithRetryStopsOnContextCancel проверяет остановку ретраев по отменённому контексту.
// Ожидает ошибку и ни одного успешного вызова Seed.
func TestProcessWithRetryStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	processor := &fakeProcessor{}
	consumer := testConsumer(processor)

	if err := consumer.processWithRetry(ctx, []domain.PublishedPost{{PostID: "post-1"}}); err == nil {
		t.Fatal("processWithRetry() error = nil, want context.Canceled")
	}
	if len(processor.calls) != 0 {
		t.Fatalf("seed calls = %d, want 0", len(processor.calls))
	}
}

// TestCommitMessagesMarksAndCommits проверяет коммит offset'ов обработанных сообщений.
// Ожидает MarkMessage каждого сообщения через его сессию и один Commit.
func TestCommitMessagesMarksAndCommits(t *testing.T) {
	t.Parallel()

	session := &fakeGroupSession{}
	envelopes := []saramaEnvelope{
		{msg: &sarama.ConsumerMessage{Topic: "post-events", Partition: 2, Offset: 15}, session: session},
	}
	consumer := testConsumer(&fakeProcessor{})

	if err := consumer.commitMessages(context.Background(), envelopes); err != nil {
		t.Fatalf("commitMessages() error = %v", err)
	}

	if len(session.marked) != 1 || session.marked[0].Offset != 15 {
		t.Fatalf("marked = %v, want one message with offset 15", session.marked)
	}
	if session.commits != 1 {
		t.Fatalf("commits = %d, want 1", session.commits)
	}
}

// TestCommitMessagesChecksContext проверяет проверку контекста при коммите.
// Ожидает ошибку context.Canceled без вызовов Commit.
func TestCommitMessagesChecksContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	session := &fakeGroupSession{}
	consumer := testConsumer(&fakeProcessor{})

	err := consumer.commitMessages(ctx, []saramaEnvelope{
		{msg: &sarama.ConsumerMessage{Topic: "post-events", Partition: 2, Offset: 15}, session: session},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("commitMessages() error = %v, want context.Canceled", err)
	}
	if session.commits != 0 {
		t.Fatalf("commits = %d, want 0", session.commits)
	}
}

// TestRunProcessesCommitsAndStops проверяет полный цикл Run: сборку батча,
// обработку процессором и коммит offset'ов.
// Ожидает Seed с разобранными постами, MarkMessage и Commit через сессию,
// остановку цикла по отмене контекста.
func TestRunProcessesCommitsAndStops(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	post := domain.PublishedPost{PostID: "post-1", AuthorID: "author-1", CreatedAtUnixMicro: 100}
	session := &fakeGroupSession{}
	processor := &fakeProcessor{}
	consumer := testConsumer(processor)
	env := testEnvelope(t, post)
	env.session = session
	consumer.msgs <- env

	done := make(chan struct{})
	go func() {
		defer close(done)
		consumer.Run(ctx)
	}()

	waitFor(t, func() bool {
		return len(processor.calls) == 1 && session.commits == 1
	})
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not stop on context cancel")
	}

	if len(processor.calls) != 1 || len(processor.calls[0]) != 1 || processor.calls[0][0] != post {
		t.Fatalf("seed calls = %v, want one batch with %v", processor.calls, post)
	}
	if len(session.marked) != 1 || session.marked[0].Offset != env.msg.Offset {
		t.Fatalf("marked = %v, want one message with offset %d", session.marked, env.msg.Offset)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met")
}

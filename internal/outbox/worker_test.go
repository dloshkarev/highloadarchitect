package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dloshkarev/highloadarchitect/internal/config"
	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

const (
	testPostID   = "post-1"
	testAuthorID = "author-1"
)

func testConfig() config.OutboxConfig {
	return config.OutboxConfig{PollInterval: time.Hour, BatchSize: 10}
}

type recorder struct {
	calls []string
}

func (r *recorder) add(call string) {
	if r != nil {
		r.calls = append(r.calls, call)
	}
}

type fakeOutboxRepository struct {
	events    []domain.OutboxEvent
	getErr    error
	deleteErr error
	getCalls  int
	limit     int
	deleted   []int64
}

func (f *fakeOutboxRepository) GetBatch(_ context.Context, limit int) ([]domain.OutboxEvent, error) {
	f.getCalls++
	f.limit = limit
	if f.getErr != nil {
		return nil, f.getErr
	}

	return f.events, nil
}

func (f *fakeOutboxRepository) Delete(_ context.Context, id int64) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, id)

	return nil
}

type fakeProducer struct {
	rec   *recorder
	err   error
	posts []domain.PublishedPost
}

func (f *fakeProducer) Publish(_ context.Context, post domain.PublishedPost) error {
	f.rec.add("publish")
	if f.err != nil {
		return f.err
	}
	f.posts = append(f.posts, post)

	return nil
}

type fakeCache struct {
	rec        *recorder
	err        error
	put        []domain.Post
	updated    []domain.Post
	tombstoned []string
}

func (f *fakeCache) PutPostIfAbsent(_ context.Context, post domain.Post) error {
	f.rec.add("put")
	if f.err != nil {
		return f.err
	}
	f.put = append(f.put, post)

	return nil
}

func (f *fakeCache) UpdatePostIfNotDeleted(_ context.Context, post domain.Post) error {
	f.rec.add("update")
	if f.err != nil {
		return f.err
	}
	f.updated = append(f.updated, post)

	return nil
}

func (f *fakeCache) Tombstone(_ context.Context, postID string) error {
	f.rec.add("tombstone")
	if f.err != nil {
		return f.err
	}
	f.tombstoned = append(f.tombstoned, postID)

	return nil
}

func TestProcessNewPost(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	createdAt := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	event := domain.OutboxEvent{
		ID:   7,
		Type: domain.OutboxEventNewPost,
		Payload: marshalPayload(t, domain.NewPostPayload{
			PostID:             testPostID,
			AuthorID:           testAuthorID,
			Body:               "hello",
			CreatedAtUnixMicro: createdAt.UnixMicro(),
		}),
	}
	repo := &fakeOutboxRepository{events: []domain.OutboxEvent{event}}
	rec := &recorder{}
	producer := &fakeProducer{rec: rec}
	cache := &fakeCache{rec: rec}
	worker := NewWorker(repo, producer, cache, testConfig())

	worker.process(ctx)

	assertCalls(t, rec.calls, []string{"publish", "put"})

	if len(producer.posts) != 1 {
		t.Fatalf("published = %d, want 1", len(producer.posts))
	}
	wantPublished := domain.PublishedPost{
		PostID:             testPostID,
		AuthorID:           testAuthorID,
		CreatedAtUnixMicro: createdAt.UnixMicro(),
	}
	if producer.posts[0] != wantPublished {
		t.Fatalf("published post = %+v, want %+v", producer.posts[0], wantPublished)
	}

	if len(cache.put) != 1 {
		t.Fatalf("put posts = %d, want 1", len(cache.put))
	}
	post := cache.put[0]
	if post.ID != testPostID || post.AuthorID != testAuthorID || post.Text != "hello" {
		t.Fatalf("cached post = %+v", post)
	}
	if !post.CreatedAt.Equal(createdAt) {
		t.Fatalf("cached created_at = %s, want %s", post.CreatedAt, createdAt)
	}

	assertDeleted(t, repo.deleted, []int64{7})
}

func TestProcessEditPost(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	event := domain.OutboxEvent{
		ID:   8,
		Type: domain.OutboxEventEditPost,
		Payload: marshalPayload(t, domain.EditPostPayload{
			PostID:   testPostID,
			AuthorID: testAuthorID,
			Body:     "edited",
		}),
	}
	repo := &fakeOutboxRepository{events: []domain.OutboxEvent{event}}
	rec := &recorder{}
	cache := &fakeCache{rec: rec}
	worker := NewWorker(repo, &fakeProducer{rec: rec}, cache, testConfig())

	worker.process(ctx)

	assertCalls(t, rec.calls, []string{"update"})

	if len(cache.updated) != 1 {
		t.Fatalf("updated posts = %d, want 1", len(cache.updated))
	}
	post := cache.updated[0]
	if post.ID != testPostID || post.AuthorID != testAuthorID || post.Text != "edited" {
		t.Fatalf("updated post = %+v", post)
	}

	assertDeleted(t, repo.deleted, []int64{8})
}

func TestProcessDeletePost(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	event := domain.OutboxEvent{
		ID:   9,
		Type: domain.OutboxEventDeletePost,
		Payload: marshalPayload(t, domain.DeletePostPayload{
			PostID:   testPostID,
			AuthorID: testAuthorID,
		}),
	}
	repo := &fakeOutboxRepository{events: []domain.OutboxEvent{event}}
	rec := &recorder{}
	cache := &fakeCache{rec: rec}
	worker := NewWorker(repo, &fakeProducer{rec: rec}, cache, testConfig())

	worker.process(ctx)

	assertCalls(t, rec.calls, []string{"tombstone"})
	assertCalls(t, cache.tombstoned, []string{testPostID})
	assertDeleted(t, repo.deleted, []int64{9})
}

func TestProcessProducerErrorSkipsCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	event := domain.OutboxEvent{
		ID:   10,
		Type: domain.OutboxEventNewPost,
		Payload: marshalPayload(t, domain.NewPostPayload{
			PostID:   testPostID,
			AuthorID: testAuthorID,
			Body:     "hello",
		}),
	}
	repo := &fakeOutboxRepository{events: []domain.OutboxEvent{event}}
	rec := &recorder{}
	producer := &fakeProducer{rec: rec, err: errors.New("kafka is down")}
	cache := &fakeCache{rec: rec}
	worker := NewWorker(repo, producer, cache, testConfig())

	worker.process(ctx)

	assertCalls(t, rec.calls, []string{"publish"})
	if len(cache.put) != 0 {
		t.Fatalf("put posts = %d, want 0", len(cache.put))
	}
	assertDeleted(t, repo.deleted, []int64{10})
}

func TestProcessContinuesAfterInvalidPayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	events := []domain.OutboxEvent{
		{ID: 11, Type: domain.OutboxEventNewPost, Payload: []byte("{")},
		{
			ID:      12,
			Type:    domain.OutboxEventDeletePost,
			Payload: marshalPayload(t, domain.DeletePostPayload{PostID: testPostID, AuthorID: testAuthorID}),
		},
	}
	repo := &fakeOutboxRepository{events: events}
	rec := &recorder{}
	producer := &fakeProducer{rec: rec}
	cache := &fakeCache{rec: rec}
	worker := NewWorker(repo, producer, cache, testConfig())

	worker.process(ctx)

	assertCalls(t, rec.calls, []string{"tombstone"})
	if len(producer.posts) != 0 {
		t.Fatalf("published = %d, want 0", len(producer.posts))
	}
	assertDeleted(t, repo.deleted, []int64{11, 12})
}

func TestProcessUnknownEventType(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := &fakeOutboxRepository{events: []domain.OutboxEvent{{ID: 13, Type: "unknown"}}}
	rec := &recorder{}
	worker := NewWorker(repo, &fakeProducer{rec: rec}, &fakeCache{rec: rec}, testConfig())

	worker.process(ctx)

	if len(rec.calls) != 0 {
		t.Fatalf("calls = %v, want none", rec.calls)
	}
	assertDeleted(t, repo.deleted, []int64{13})
}

func TestProcessBatchError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := &fakeOutboxRepository{getErr: errors.New("db is down")}
	worker := NewWorker(repo, nil, nil, testConfig())

	worker.process(ctx)

	if repo.getCalls != 1 {
		t.Fatalf("get calls = %d, want 1", repo.getCalls)
	}
	if len(repo.deleted) != 0 {
		t.Fatalf("deleted = %v, want none", repo.deleted)
	}
}

func TestProcessUsesBatchSize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := &fakeOutboxRepository{}
	worker := NewWorker(repo, nil, nil, config.OutboxConfig{PollInterval: time.Hour, BatchSize: 42})

	worker.process(ctx)

	if repo.limit != 42 {
		t.Fatalf("batch limit = %d, want 42", repo.limit)
	}
}

func TestProcessSkipsWhenBusy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := &fakeOutboxRepository{}
	worker := NewWorker(repo, nil, nil, testConfig())

	worker.mu.Lock()
	worker.process(ctx)
	worker.mu.Unlock()

	if repo.getCalls != 0 {
		t.Fatalf("get calls = %d, want 0", repo.getCalls)
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		NewWorker(&fakeOutboxRepository{}, nil, nil, testConfig()).Run(ctx)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop on context cancel")
	}
}

func marshalPayload(t *testing.T, payload any) []byte {
	t.Helper()

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	return data
}

func assertCalls(t *testing.T, got []string, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls = %v, want %v", got, want)
		}
	}
}

func assertDeleted(t *testing.T, got []int64, want []int64) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("deleted = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("deleted = %v, want %v", got, want)
		}
	}
}

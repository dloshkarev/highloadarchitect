package feed

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/dloshkarev/highloadarchitect/internal/config"
	"github.com/dloshkarev/highloadarchitect/internal/domain"
	rediscache "github.com/dloshkarev/highloadarchitect/internal/redis"
)

func testFeedConfig() config.FeedConfig {
	return config.FeedConfig{
		Enabled:        true,
		CacheLimit:     1000,
		WarmLimit:      1000,
		TTL:            time.Hour,
		PostTTL:        3 * time.Hour,
		EmptyMarkerTTL: time.Hour,
	}
}

func newTestService(t *testing.T, source PostSource) (*Service, *rediscache.Cache) {
	t.Helper()

	return newTestServiceWithConfig(t, source, testFeedConfig())
}

func newTestServiceWithConfig(
	t *testing.T,
	source PostSource,
	cfg config.FeedConfig,
) (*Service, *rediscache.Cache) {
	t.Helper()

	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	cache := rediscache.NewCache(rdb, cfg)

	return New(cache, source, cfg), cache
}

func testPost(id string) domain.Post {
	return domain.Post{
		ID:        id,
		AuthorID:  "author",
		Text:      "text-" + id,
		CreatedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}
}

type feedCall struct {
	userID string
	offset int
	limit  int
}

type fakePostSource struct {
	posts     map[string]domain.Post
	feedPosts []domain.Post
	feedErr   error
	err       error
	reverse   bool
	calls     [][]string
	feedCalls []feedCall
}

func (f *fakePostSource) Feed(_ context.Context, userID string, offset int, limit int) ([]domain.Post, error) {
	f.feedCalls = append(f.feedCalls, feedCall{userID: userID, offset: offset, limit: limit})
	if f.feedErr != nil {
		return nil, f.feedErr
	}

	return f.feedPosts, nil
}

func (f *fakePostSource) GetByIDs(_ context.Context, ids []string) ([]domain.Post, error) {
	f.calls = append(f.calls, append([]string(nil), ids...))
	if f.err != nil {
		return nil, f.err
	}

	result := make([]domain.Post, 0, len(ids))
	for _, id := range ids {
		if post, ok := f.posts[id]; ok {
			result = append(result, post)
		}
	}
	if f.reverse {
		for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
			result[i], result[j] = result[j], result[i]
		}
	}

	return result, nil
}

func TestLoadPostsAllCached(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	source := &fakePostSource{posts: map[string]domain.Post{}}
	service, cache := newTestService(t, source)

	first := testPost("first")
	second := testPost("second")
	if err := cache.PutPostIfAbsent(ctx, first); err != nil {
		t.Fatalf("PutPostIfAbsent() error = %v", err)
	}
	if err := cache.PutPostIfAbsent(ctx, second); err != nil {
		t.Fatalf("PutPostIfAbsent() error = %v", err)
	}

	posts, err := service.loadPosts(ctx, []string{first.ID, second.ID})
	if err != nil {
		t.Fatalf("loadPosts() error = %v", err)
	}
	if len(source.calls) != 0 {
		t.Fatalf("GetByIDs() called %d times, want 0", len(source.calls))
	}
	assertPostIDs(t, posts, []string{first.ID, second.ID})
}

func TestLoadPostsFillsMissingAndSkipsDeleted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	fromDB := testPost("from-db")
	source := &fakePostSource{
		posts:   map[string]domain.Post{fromDB.ID: fromDB},
		reverse: true,
	}
	service, cache := newTestService(t, source)

	cached := testPost("cached")
	deleted := testPost("deleted")
	if err := cache.PutPostIfAbsent(ctx, cached); err != nil {
		t.Fatalf("PutPostIfAbsent() error = %v", err)
	}
	if err := cache.Tombstone(ctx, deleted.ID); err != nil {
		t.Fatalf("Tombstone() error = %v", err)
	}

	posts, err := service.loadPosts(ctx, []string{cached.ID, deleted.ID, fromDB.ID, "absent"})
	if err != nil {
		t.Fatalf("loadPosts() error = %v", err)
	}
	assertPostIDs(t, posts, []string{cached.ID, fromDB.ID})

	if len(source.calls) != 1 {
		t.Fatalf("GetByIDs() calls = %d, want 1", len(source.calls))
	}
	assertStrings(t, source.calls[0], []string{fromDB.ID, "absent"})

	bodies, _, missing, err := cache.GetPosts(ctx, []string{fromDB.ID})
	if err != nil {
		t.Fatalf("GetPosts() error = %v", err)
	}
	if _, ok := bodies[fromDB.ID]; !ok {
		t.Fatal("post from DB was not cached")
	}
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want empty", missing)
	}
}

func TestLoadPostsOrder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	first := testPost("first")
	second := testPost("second")
	source := &fakePostSource{
		posts:   map[string]domain.Post{first.ID: first, second.ID: second},
		reverse: true,
	}
	service, _ := newTestService(t, source)

	posts, err := service.loadPosts(ctx, []string{second.ID, first.ID})
	if err != nil {
		t.Fatalf("loadPosts() error = %v", err)
	}
	assertPostIDs(t, posts, []string{second.ID, first.ID})
}

func TestLoadPostsDBError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	errDB := errors.New("db is down")
	source := &fakePostSource{err: errDB}
	service, _ := newTestService(t, source)

	_, err := service.loadPosts(ctx, []string{"post-1"})
	if !errors.Is(err, errDB) {
		t.Fatalf("loadPosts() error = %v, want %v", err, errDB)
	}
}

func TestLoadPostsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	source := &fakePostSource{}
	service, _ := newTestService(t, source)

	posts, err := service.loadPosts(ctx, nil)
	if err != nil {
		t.Fatalf("loadPosts() error = %v", err)
	}
	if len(posts) != 0 {
		t.Fatalf("loadPosts() = %v, want empty", posts)
	}
}

func seedCache(t *testing.T, cache *rediscache.Cache, userID string, posts []domain.Post) {
	t.Helper()

	ctx := context.Background()
	if err := cache.Warm(ctx, userID, posts); err != nil {
		t.Fatalf("Warm() error = %v", err)
	}
	for _, post := range posts {
		if err := cache.PutPostIfAbsent(ctx, post); err != nil {
			t.Fatalf("PutPostIfAbsent() error = %v", err)
		}
	}
}

func TestFeedDisabledUsesDB(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	posts := []domain.Post{testPost("second"), testPost("first")}
	cfg := testFeedConfig()
	cfg.Enabled = false
	source := &fakePostSource{feedPosts: posts}
	service, cache := newTestServiceWithConfig(t, source, cfg)

	got, err := service.Feed(ctx, "user-1", 0, 2)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	assertPostIDs(t, got, []string{"second", "first"})

	if len(source.feedCalls) != 1 {
		t.Fatalf("Feed() calls = %d, want 1", len(source.feedCalls))
	}
	card, err := cache.Card(ctx, "user-1")
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card != 0 {
		t.Fatalf("Card() = %d, want 0", card)
	}
}

func TestFeedWarmsEmptyCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	first := testPost("first")
	second := testPost("second")
	source := &fakePostSource{
		feedPosts: []domain.Post{second, first},
		posts:     map[string]domain.Post{first.ID: first, second.ID: second},
	}
	service, cache := newTestService(t, source)

	got, err := service.Feed(ctx, "user-1", 0, 2)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	assertPostIDs(t, got, []string{second.ID, first.ID})

	if len(source.feedCalls) != 1 {
		t.Fatalf("Feed() calls = %d, want 1", len(source.feedCalls))
	}
	if call := source.feedCalls[0]; call.offset != 0 || call.limit != testFeedConfig().WarmLimit {
		t.Fatalf("warm Feed(%d, %d), want (0, %d)", call.offset, call.limit, testFeedConfig().WarmLimit)
	}
	if len(source.calls) != 1 {
		t.Fatalf("GetByIDs() calls = %d, want 1", len(source.calls))
	}

	card, err := cache.Card(ctx, "user-1")
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card != 2 {
		t.Fatalf("Card() = %d, want 2", card)
	}
}

func TestFeedCacheHit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	first := testPost("first")
	second := testPost("second")
	third := testPost("third")
	service, cache := newTestService(t, &fakePostSource{})
	seedCache(t, cache, "user-1", []domain.Post{first, second, third})

	got, err := service.Feed(ctx, "user-1", 1, 2)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	assertPostIDs(t, got, []string{second.ID, first.ID})
}

func TestFeedRangeBeyondCacheUsesDB(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	first := testPost("first")
	second := testPost("second")
	fromDB := []domain.Post{testPost("db-3"), testPost("db-2")}
	source := &fakePostSource{feedPosts: fromDB}
	service, cache := newTestService(t, source)
	seedCache(t, cache, "user-1", []domain.Post{first, second})

	got, err := service.Feed(ctx, "user-1", 1, 5)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	assertPostIDs(t, got, []string{"db-3", "db-2"})

	if len(source.feedCalls) != 1 {
		t.Fatalf("Feed() calls = %d, want 1", len(source.feedCalls))
	}
	if call := source.feedCalls[0]; call.offset != 1 || call.limit != 5 {
		t.Fatalf("Feed(%d, %d), want (1, 5)", call.offset, call.limit)
	}
	if len(source.calls) != 0 {
		t.Fatalf("GetByIDs() calls = %d, want 0", len(source.calls))
	}
}

func TestFeedEmptyDB(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	source := &fakePostSource{}
	service, cache := newTestService(t, source)

	got, err := service.Feed(ctx, "user-1", 0, 10)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Feed() = %v, want empty", got)
	}
	if len(source.feedCalls) != 1 {
		t.Fatalf("Feed() calls = %d, want 1", len(source.feedCalls))
	}
	card, err := cache.Card(ctx, "user-1")
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card != 0 {
		t.Fatalf("Card() = %d, want 0", card)
	}
}

func TestFeedWarmError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	errDB := errors.New("db is down")
	source := &fakePostSource{feedErr: errDB}
	service, _ := newTestService(t, source)

	_, err := service.Feed(ctx, "user-1", 0, 10)
	if !errors.Is(err, errDB) {
		t.Fatalf("Feed() error = %v, want %v", err, errDB)
	}
}

func assertPostIDs(t *testing.T, posts []domain.Post, want []string) {
	t.Helper()

	got := make([]string, len(posts))
	for i, post := range posts {
		got[i] = post.ID
	}
	assertStrings(t, got, want)
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

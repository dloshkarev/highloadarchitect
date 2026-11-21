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

	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	cache := rediscache.NewCache(rdb, testFeedConfig())

	return New(cache, source), cache
}

func testPost(id string) domain.Post {
	return domain.Post{
		ID:        id,
		AuthorID:  "author",
		Text:      "text-" + id,
		CreatedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}
}

type fakePostSource struct {
	posts   map[string]domain.Post
	err     error
	reverse bool
	calls   [][]string
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

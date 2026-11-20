package redis

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/dloshkarev/highloadarchitect/internal/config"
	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

func testFeedConfig() config.FeedConfig {
	return config.FeedConfig{
		Enabled:        true,
		CacheLimit:     3,
		WarmLimit:      1000,
		TTL:            time.Hour,
		PostTTL:        3 * time.Hour,
		EmptyMarkerTTL: time.Hour,
	}
}

func newTestCache(t *testing.T, cfg config.FeedConfig) (*Cache, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	return NewCache(rdb, cfg), mr
}

func testPosts(count int) []domain.Post {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	posts := make([]domain.Post, 0, count)
	for i := range count {
		posts = append(posts, domain.Post{
			ID:        "post-" + string(rune('a'+i)),
			AuthorID:  "author",
			Text:      "text-" + string(rune('a'+i)),
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		})
	}

	return posts
}

func TestKeys(t *testing.T) {
	t.Parallel()

	if got := FeedKey("user-1"); got != "feed:user-1" {
		t.Fatalf("FeedKey() = %q, want %q", got, "feed:user-1")
	}
	if got := PostKey("post-1"); got != "post:post-1" {
		t.Fatalf("PostKey() = %q, want %q", got, "post:post-1")
	}
}

func TestWarmAndRange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache, mr := newTestCache(t, testFeedConfig())
	posts := testPosts(3)

	if err := cache.Warm(ctx, "user-1", posts); err != nil {
		t.Fatalf("Warm() error = %v", err)
	}

	card, err := cache.Card(ctx, "user-1")
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card != 3 {
		t.Fatalf("Card() = %d, want 3", card)
	}

	if ttl := mr.TTL(FeedKey("user-1")); ttl != testFeedConfig().TTL {
		t.Fatalf("feed TTL = %v, want %v", ttl, testFeedConfig().TTL)
	}

	score, err := mr.ZScore(FeedKey("user-1"), posts[0].ID)
	if err != nil {
		t.Fatalf("ZScore() error = %v", err)
	}
	if want := float64(posts[0].CreatedAt.UnixMicro()); score != want {
		t.Fatalf("score = %v, want %v", score, want)
	}

	ids, err := cache.Range(ctx, "user-1", 0, 2)
	if err != nil {
		t.Fatalf("Range() error = %v", err)
	}
	assertIDs(t, ids, []string{posts[2].ID, posts[1].ID})

	ids, err = cache.Range(ctx, "user-1", 1, 2)
	if err != nil {
		t.Fatalf("Range() error = %v", err)
	}
	assertIDs(t, ids, []string{posts[1].ID, posts[0].ID})
}

func TestWarmEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache, mr := newTestCache(t, testFeedConfig())

	if err := cache.Warm(ctx, "user-1", nil); err != nil {
		t.Fatalf("Warm() error = %v", err)
	}
	if mr.Exists(FeedKey("user-1")) {
		t.Fatal("Warm() created key for empty feed")
	}
}

func TestTouch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache, mr := newTestCache(t, testFeedConfig())
	posts := testPosts(1)

	if err := cache.Warm(ctx, "user-1", posts); err != nil {
		t.Fatalf("Warm() error = %v", err)
	}
	mr.SetTTL(FeedKey("user-1"), time.Minute)

	if err := cache.Touch(ctx, "user-1"); err != nil {
		t.Fatalf("Touch() error = %v", err)
	}
	if ttl := mr.TTL(FeedKey("user-1")); ttl != testFeedConfig().TTL {
		t.Fatalf("feed TTL = %v, want %v", ttl, testFeedConfig().TTL)
	}

	if err := cache.Touch(ctx, "missing-user"); err != nil {
		t.Fatalf("Touch() missing key error = %v", err)
	}
}

func TestGetPosts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache, _ := newTestCache(t, testFeedConfig())
	posts := testPosts(3)

	if err := cache.PutPostIfAbsent(ctx, posts[0]); err != nil {
		t.Fatalf("PutPostIfAbsent() error = %v", err)
	}
	if err := cache.PutPostIfAbsent(ctx, posts[1]); err != nil {
		t.Fatalf("PutPostIfAbsent() error = %v", err)
	}
	if err := cache.Tombstone(ctx, posts[2].ID); err != nil {
		t.Fatalf("Tombstone() error = %v", err)
	}

	bodies, deleted, missing, err := cache.GetPosts(ctx, []string{posts[0].ID, posts[2].ID, "missing", posts[1].ID})
	if err != nil {
		t.Fatalf("GetPosts() error = %v", err)
	}

	if len(bodies) != 2 {
		t.Fatalf("bodies count = %d, want 2", len(bodies))
	}
	if got := bodies[posts[0].ID]; got.Text != posts[0].Text || !got.CreatedAt.Equal(posts[0].CreatedAt) {
		t.Fatalf("body[%s] = %+v, want %+v", posts[0].ID, got, posts[0])
	}
	if _, ok := deleted[posts[2].ID]; !ok {
		t.Fatalf("deleted does not contain %s", posts[2].ID)
	}
	assertIDs(t, missing, []string{"missing"})
}

func TestPutPostIfAbsentDoesNotOverwrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache, _ := newTestCache(t, testFeedConfig())
	post := testPosts(1)[0]

	if err := cache.PutPostIfAbsent(ctx, post); err != nil {
		t.Fatalf("PutPostIfAbsent() error = %v", err)
	}

	updated := post
	updated.Text = "new text"
	if err := cache.PutPostIfAbsent(ctx, updated); err != nil {
		t.Fatalf("PutPostIfAbsent() second error = %v", err)
	}

	bodies, _, _, err := cache.GetPosts(ctx, []string{post.ID})
	if err != nil {
		t.Fatalf("GetPosts() error = %v", err)
	}
	if got := bodies[post.ID].Text; got != post.Text {
		t.Fatalf("cached text = %q, want %q", got, post.Text)
	}
}

func TestPutPostIfAbsentDoesNotOverwriteTombstone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache, _ := newTestCache(t, testFeedConfig())
	post := testPosts(1)[0]

	if err := cache.Tombstone(ctx, post.ID); err != nil {
		t.Fatalf("Tombstone() error = %v", err)
	}
	if err := cache.PutPostIfAbsent(ctx, post); err != nil {
		t.Fatalf("PutPostIfAbsent() error = %v", err)
	}

	_, deleted, _, err := cache.GetPosts(ctx, []string{post.ID})
	if err != nil {
		t.Fatalf("GetPosts() error = %v", err)
	}
	if _, ok := deleted[post.ID]; !ok {
		t.Fatal("tombstone was overwritten by PutPostIfAbsent")
	}
}

func TestUpdatePostIfNotDeleted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache, _ := newTestCache(t, testFeedConfig())
	post := testPosts(1)[0]

	if err := cache.PutPostIfAbsent(ctx, post); err != nil {
		t.Fatalf("PutPostIfAbsent() error = %v", err)
	}

	updated := post
	updated.Text = "edited"
	if err := cache.UpdatePostIfNotDeleted(ctx, updated); err != nil {
		t.Fatalf("UpdatePostIfNotDeleted() error = %v", err)
	}

	bodies, _, _, err := cache.GetPosts(ctx, []string{post.ID})
	if err != nil {
		t.Fatalf("GetPosts() error = %v", err)
	}
	if got := bodies[post.ID].Text; got != "edited" {
		t.Fatalf("cached text = %q, want %q", got, "edited")
	}
}

func TestUpdatePostDoesNotOverwriteTombstone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache, _ := newTestCache(t, testFeedConfig())
	post := testPosts(1)[0]

	if err := cache.Tombstone(ctx, post.ID); err != nil {
		t.Fatalf("Tombstone() error = %v", err)
	}
	if err := cache.UpdatePostIfNotDeleted(ctx, post); err != nil {
		t.Fatalf("UpdatePostIfNotDeleted() error = %v", err)
	}

	_, deleted, _, err := cache.GetPosts(ctx, []string{post.ID})
	if err != nil {
		t.Fatalf("GetPosts() error = %v", err)
	}
	if _, ok := deleted[post.ID]; !ok {
		t.Fatal("tombstone was overwritten by UpdatePostIfNotDeleted")
	}
}

func TestUpdatePostIfNotDeletedCreatesMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache, _ := newTestCache(t, testFeedConfig())
	post := testPosts(1)[0]
	post.Text = "edited"

	if err := cache.UpdatePostIfNotDeleted(ctx, post); err != nil {
		t.Fatalf("UpdatePostIfNotDeleted() error = %v", err)
	}

	bodies, _, _, err := cache.GetPosts(ctx, []string{post.ID})
	if err != nil {
		t.Fatalf("GetPosts() error = %v", err)
	}
	if got := bodies[post.ID].Text; got != "edited" {
		t.Fatalf("cached text = %q, want %q", got, "edited")
	}
}

func TestFanout(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := testFeedConfig()
	cache, mr := newTestCache(t, cfg)
	posts := testPosts(3)

	if err := cache.Warm(ctx, "user-1", posts); err != nil {
		t.Fatalf("Warm() error = %v", err)
	}

	newItems := []FanoutItem{
		{PostID: "new-1", Score: posts[2].CreatedAt.UnixMicro() + 1},
		{PostID: "new-2", Score: posts[2].CreatedAt.UnixMicro() + 2},
	}
	if err := cache.Fanout(ctx, "user-1", newItems); err != nil {
		t.Fatalf("Fanout() error = %v", err)
	}

	ids, err := cache.Range(ctx, "user-1", 0, int(cfg.CacheLimit))
	if err != nil {
		t.Fatalf("Range() error = %v", err)
	}
	assertIDs(t, ids, []string{"new-2", "new-1", posts[2].ID})

	// Повторный fanout того же поста не создаёт дубль.
	if err := cache.Fanout(ctx, "user-1", newItems[:1]); err != nil {
		t.Fatalf("Fanout() repeat error = %v", err)
	}
	card, err := cache.Card(ctx, "user-1")
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card != int64(cfg.CacheLimit) {
		t.Fatalf("Card() = %d, want %d", card, cfg.CacheLimit)
	}

	// Неактивному пользователю лента не создаётся.
	if err := cache.Fanout(ctx, "inactive", newItems); err != nil {
		t.Fatalf("Fanout() inactive error = %v", err)
	}
	if mr.Exists(FeedKey("inactive")) {
		t.Fatal("Fanout() created feed for inactive user")
	}
}

func TestFanoutEmptyItems(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache, mr := newTestCache(t, testFeedConfig())

	if err := cache.Fanout(ctx, "user-1", nil); err != nil {
		t.Fatalf("Fanout() error = %v", err)
	}
	if mr.Exists(FeedKey("user-1")) {
		t.Fatal("Fanout() created feed for empty items")
	}
}

func TestOpen(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	ctx := context.Background()

	cache, err := Open(ctx, config.RedisConfig{
		Addr:         mr.Addr(),
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
		PoolSize:     1,
	}, testFeedConfig())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if err := cache.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	if err := cache.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func assertIDs(t *testing.T, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	}
}

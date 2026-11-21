package feed

import (
	"context"
	"errors"
	"testing"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
	rediscache "github.com/dloshkarev/highloadarchitect/internal/redis"
)

type fakeFollowerSource struct {
	followers map[string][]string
	err       error
	calls     [][]string
}

func (f *fakeFollowerSource) Followers(_ context.Context, authorIDs []string) (map[string][]string, error) {
	f.calls = append(f.calls, authorIDs)
	if f.err != nil {
		return nil, f.err
	}

	return f.followers, nil
}

type fakeFanoutCache struct {
	items map[string][]rediscache.FanoutItem
	calls []string
	err   error
}

func (f *fakeFanoutCache) Fanout(_ context.Context, userID string, items []rediscache.FanoutItem) error {
	f.calls = append(f.calls, userID)
	if f.err != nil {
		return f.err
	}
	if f.items == nil {
		f.items = make(map[string][]rediscache.FanoutItem)
	}
	f.items[userID] = append(f.items[userID], items...)

	return nil
}

// TestSeedGroupsByRecipient проверяет группировку постов батча по получателям.
// Ожидает один вызов Followers со списком авторов без повторов и fanout
// каждому другу только постов его авторов.
func TestSeedGroupsByRecipient(t *testing.T) {
	t.Parallel()

	posts := []domain.PublishedPost{
		{PostID: "post-1", AuthorID: "author-1", CreatedAtUnixMicro: 100},
		{PostID: "post-2", AuthorID: "author-1", CreatedAtUnixMicro: 200},
		{PostID: "post-3", AuthorID: "author-2", CreatedAtUnixMicro: 300},
	}
	friends := &fakeFollowerSource{followers: map[string][]string{
		"author-1": {"u1", "u2"},
		"author-2": {"u2", "u3"},
	}}
	cache := &fakeFanoutCache{}
	seeder := NewSeeder(friends, cache)

	if err := seeder.Seed(context.Background(), posts); err != nil {
		t.Fatalf("Seed() error = %v", err)
	}

	if len(friends.calls) != 1 {
		t.Fatalf("followers calls = %d, want 1", len(friends.calls))
	}
	assertStrings(t, friends.calls[0], []string{"author-1", "author-2"})
	assertFanout(t, cache.items, map[string][]rediscache.FanoutItem{
		"u1": {
			{PostID: "post-1", Score: 100},
			{PostID: "post-2", Score: 200},
		},
		"u2": {
			{PostID: "post-1", Score: 100},
			{PostID: "post-2", Score: 200},
			{PostID: "post-3", Score: 300},
		},
		"u3": {
			{PostID: "post-3", Score: 300},
		},
	})
}

// TestSeedQueriesFollowersOncePerBatch проверяет, что на несколько постов
// одного автора списки друзей запрашиваются один раз.
// Ожидает ровно один вызов Followers со списком из одного автора.
func TestSeedQueriesFollowersOncePerBatch(t *testing.T) {
	t.Parallel()

	posts := []domain.PublishedPost{
		{PostID: "post-1", AuthorID: "author-1"},
		{PostID: "post-2", AuthorID: "author-1"},
		{PostID: "post-3", AuthorID: "author-1"},
	}
	friends := &fakeFollowerSource{followers: map[string][]string{"author-1": {"u1"}}}
	cache := &fakeFanoutCache{}
	seeder := NewSeeder(friends, cache)

	if err := seeder.Seed(context.Background(), posts); err != nil {
		t.Fatalf("Seed() error = %v", err)
	}

	if len(friends.calls) != 1 {
		t.Fatalf("followers calls = %d, want 1", len(friends.calls))
	}
	assertStrings(t, friends.calls[0], []string{"author-1"})
	if len(cache.items["u1"]) != 3 {
		t.Fatalf("fanout items = %d, want 3", len(cache.items["u1"]))
	}
}

// TestSeedFollowerError проверяет обработку ошибки загрузки друзей.
// Ожидает ошибку, совпадающую с ошибкой FollowerSource.
func TestSeedFollowerError(t *testing.T) {
	t.Parallel()

	followerErr := errors.New("db is down")
	seeder := NewSeeder(
		&fakeFollowerSource{err: followerErr},
		&fakeFanoutCache{},
	)

	err := seeder.Seed(context.Background(), []domain.PublishedPost{{PostID: "post-1", AuthorID: "author-1"}})
	if !errors.Is(err, followerErr) {
		t.Fatalf("Seed() error = %v, want %v", err, followerErr)
	}
}

// TestSeedFanoutError проверяет обработку ошибки записи в кэш лент.
// Ожидает ошибку, совпадающую с ошибкой FanoutCache.
func TestSeedFanoutError(t *testing.T) {
	t.Parallel()

	fanoutErr := errors.New("redis is down")
	friends := &fakeFollowerSource{followers: map[string][]string{"author-1": {"u1"}}}
	seeder := NewSeeder(
		friends,
		&fakeFanoutCache{err: fanoutErr},
	)

	err := seeder.Seed(context.Background(), []domain.PublishedPost{{PostID: "post-1", AuthorID: "author-1"}})
	if !errors.Is(err, fanoutErr) {
		t.Fatalf("Seed() error = %v, want %v", err, fanoutErr)
	}
}

// TestSeedEmptyBatch проверяет обработку пустого батча.
// Ожидает отсутствие вызовов Followers и Fanout без ошибки.
func TestSeedEmptyBatch(t *testing.T) {
	t.Parallel()

	friends := &fakeFollowerSource{}
	cache := &fakeFanoutCache{}
	seeder := NewSeeder(friends, cache)

	if err := seeder.Seed(context.Background(), nil); err != nil {
		t.Fatalf("Seed() error = %v", err)
	}
	if len(friends.calls) != 0 {
		t.Fatalf("followers calls = %d, want 0", len(friends.calls))
	}
	if len(cache.calls) != 0 {
		t.Fatalf("fanout calls = %d, want 0", len(cache.calls))
	}
}

func assertFanout(t *testing.T, got map[string][]rediscache.FanoutItem, want map[string][]rediscache.FanoutItem) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("fanout recipients = %v, want %v", got, want)
	}
	for userID, wantItems := range want {
		gotItems := got[userID]
		if len(gotItems) != len(wantItems) {
			t.Fatalf("fanout[%s] = %v, want %v", userID, gotItems, wantItems)
		}
		for i := range wantItems {
			if gotItems[i] != wantItems[i] {
				t.Fatalf("fanout[%s] = %v, want %v", userID, gotItems, wantItems)
			}
		}
	}
}

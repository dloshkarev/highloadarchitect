package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

type feedCall struct {
	userID string
	offset int
	limit  int
}

type fakeFeedService struct {
	posts []domain.Post
	calls []feedCall
}

func (f *fakeFeedService) Feed(_ context.Context, userID string, offset int, limit int) ([]domain.Post, error) {
	f.calls = append(f.calls, feedCall{userID: userID, offset: offset, limit: limit})

	return f.posts, nil
}

type fakePostRepository struct {
	feedPosts []domain.Post
	feedCalls []feedCall
}

func (f *fakePostRepository) Create(_ context.Context, _ domain.Post) error {
	return nil
}

func (f *fakePostRepository) GetByID(_ context.Context, _ string) (domain.Post, error) {
	return domain.Post{}, nil
}

func (f *fakePostRepository) GetByIDs(_ context.Context, _ []string) ([]domain.Post, error) {
	return nil, nil
}

func (f *fakePostRepository) Feed(_ context.Context, userID string, offset int, limit int) ([]domain.Post, error) {
	f.feedCalls = append(f.feedCalls, feedCall{userID: userID, offset: offset, limit: limit})

	return f.feedPosts, nil
}

func (f *fakePostRepository) UpdateText(_ context.Context, _ string, _ string, _ string) error {
	return nil
}

func (f *fakePostRepository) Delete(_ context.Context, _ string, _ string) error {
	return nil
}

func TestPostServiceFeedDelegatesToFeedService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userID := uuid.NewString()
	posts := []domain.Post{{ID: uuid.NewString(), AuthorID: userID, Text: "post"}}
	feed := &fakeFeedService{posts: posts}
	service := NewPostService(&fakePostRepository{}, feed)

	got, err := service.Feed(ctx, userID, 5, 0)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != posts[0].ID {
		t.Fatalf("Feed() = %v, want %v", got, posts)
	}

	if len(feed.calls) != 1 {
		t.Fatalf("feed calls = %d, want 1", len(feed.calls))
	}
	if call := feed.calls[0]; call.userID != userID || call.offset != 5 || call.limit != defaultFeedLimit {
		t.Fatalf("feed call = %+v, want user=%s offset=5 limit=%d", call, userID, defaultFeedLimit)
	}

	if _, err := service.Feed(ctx, userID, 0, 1000); err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if call := feed.calls[1]; call.limit != maxFeedLimit {
		t.Fatalf("limit = %d, want %d", call.limit, maxFeedLimit)
	}
}

func TestPostServiceFeedWithoutFeedService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userID := uuid.NewString()
	posts := []domain.Post{{ID: uuid.NewString(), AuthorID: userID, Text: "post"}}
	repository := &fakePostRepository{feedPosts: posts}
	service := NewPostService(repository, nil)

	got, err := service.Feed(ctx, userID, 2, 3)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != posts[0].ID {
		t.Fatalf("Feed() = %v, want %v", got, posts)
	}
	if len(repository.feedCalls) != 1 {
		t.Fatalf("repository calls = %d, want 1", len(repository.feedCalls))
	}
	if call := repository.feedCalls[0]; call.offset != 2 || call.limit != 3 {
		t.Fatalf("repository call = %+v, want offset=2 limit=3", call)
	}
}

func TestPostServiceFeedValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userID := uuid.NewString()

	tests := []struct {
		name   string
		userID string
		offset int
		limit  int
	}{
		{name: "invalid user id", userID: "bad", offset: 0, limit: 10},
		{name: "negative offset", userID: userID, offset: -1, limit: 10},
		{name: "negative limit", userID: userID, offset: 0, limit: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			feed := &fakeFeedService{}
			service := NewPostService(&fakePostRepository{}, feed)

			_, err := service.Feed(ctx, tt.userID, tt.offset, tt.limit)
			var validationErr *domain.ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("Feed() error = %v, want ValidationError", err)
			}
			if len(feed.calls) != 0 {
				t.Fatalf("feed calls = %d, want 0", len(feed.calls))
			}
		})
	}
}

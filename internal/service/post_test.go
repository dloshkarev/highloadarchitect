package service

import (
	"context"
	"encoding/json"
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

	createErr error
	updateErr error
	deleteErr error

	createdPost domain.Post
	updatedBy   string
	updatedID   string
	updatedText string
	deletedBy   string
	deletedID   string
	events      []domain.OutboxEvent
}

func (f *fakePostRepository) CreateWithOutbox(_ context.Context, post domain.Post, event domain.OutboxEvent) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.createdPost = post
	f.events = append(f.events, event)

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

func (f *fakePostRepository) UpdateTextWithOutbox(
	_ context.Context,
	postID string,
	authorID string,
	text string,
	event domain.OutboxEvent,
) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updatedID = postID
	f.updatedBy = authorID
	f.updatedText = text
	f.events = append(f.events, event)

	return nil
}

func (f *fakePostRepository) DeleteWithOutbox(
	_ context.Context,
	postID string,
	authorID string,
	event domain.OutboxEvent,
) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletedID = postID
	f.deletedBy = authorID
	f.events = append(f.events, event)

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

func TestPostServiceCreateWritesNewPostOutbox(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	authorID := uuid.NewString()
	repository := &fakePostRepository{}
	service := NewPostService(repository, nil)

	postID, err := service.Create(ctx, authorID, "  hello world  ")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, parseErr := uuid.Parse(postID); parseErr != nil {
		t.Fatalf("Create() returned invalid id %q: %v", postID, parseErr)
	}
	if repository.createdPost.ID != postID || repository.createdPost.AuthorID != authorID {
		t.Fatalf("created post = %+v, want id=%s author=%s", repository.createdPost, postID, authorID)
	}
	if repository.createdPost.Text != "hello world" {
		t.Fatalf("created text = %q, want %q", repository.createdPost.Text, "hello world")
	}

	event := onlyEvent(t, repository)
	if event.Type != domain.OutboxEventNewPost {
		t.Fatalf("event type = %q, want %q", event.Type, domain.OutboxEventNewPost)
	}

	var payload domain.NewPostPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.PostID != postID || payload.AuthorID != authorID || payload.Body != "hello world" {
		t.Fatalf("payload = %+v", payload)
	}
	wantCreatedAt := repository.createdPost.CreatedAt.UnixMicro()
	if payload.CreatedAtUnixMicro != wantCreatedAt {
		t.Fatalf("created_at_unix_micro = %d, want %d", payload.CreatedAtUnixMicro, wantCreatedAt)
	}
}

func TestPostServiceUpdateWritesEditPostOutbox(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userID := uuid.NewString()
	postID := uuid.NewString()
	repository := &fakePostRepository{}
	service := NewPostService(repository, nil)

	if err := service.Update(ctx, userID, postID, "  new text  "); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if repository.updatedID != postID || repository.updatedBy != userID {
		t.Fatalf("updated id/author = %q/%q, want %q/%q", repository.updatedID, repository.updatedBy, postID, userID)
	}
	if repository.updatedText != "new text" {
		t.Fatalf("updated text = %q, want %q", repository.updatedText, "new text")
	}

	event := onlyEvent(t, repository)
	if event.Type != domain.OutboxEventEditPost {
		t.Fatalf("event type = %q, want %q", event.Type, domain.OutboxEventEditPost)
	}

	var payload domain.EditPostPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.PostID != postID || payload.AuthorID != userID || payload.Body != "new text" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestPostServiceDeleteWritesDeletePostOutbox(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	userID := uuid.NewString()
	postID := uuid.NewString()
	repository := &fakePostRepository{}
	service := NewPostService(repository, nil)

	if err := service.Delete(ctx, userID, postID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if repository.deletedID != postID || repository.deletedBy != userID {
		t.Fatalf("deleted id/author = %q/%q, want %q/%q", repository.deletedID, repository.deletedBy, postID, userID)
	}

	event := onlyEvent(t, repository)
	if event.Type != domain.OutboxEventDeletePost {
		t.Fatalf("event type = %q, want %q", event.Type, domain.OutboxEventDeletePost)
	}

	var payload domain.DeletePostPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.PostID != postID || payload.AuthorID != userID {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestPostServiceWriteValidationSkipsRepository(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := &fakePostRepository{}
	service := NewPostService(repository, nil)

	if _, err := service.Create(ctx, uuid.NewString(), "   "); err == nil {
		t.Fatal("Create() error = nil, want validation error")
	}
	if err := service.Update(ctx, uuid.NewString(), "bad", "text"); err == nil {
		t.Fatal("Update() error = nil, want validation error")
	}
	if err := service.Delete(ctx, uuid.NewString(), "bad"); err == nil {
		t.Fatal("Delete() error = nil, want validation error")
	}
	if len(repository.events) != 0 {
		t.Fatalf("outbox events = %d, want 0", len(repository.events))
	}
}

func onlyEvent(t *testing.T, repository *fakePostRepository) domain.OutboxEvent {
	t.Helper()

	if len(repository.events) != 1 {
		t.Fatalf("outbox events = %d, want 1", len(repository.events))
	}

	return repository.events[0]
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

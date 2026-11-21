package repository

import (
	"context"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

type UserRepository interface {
	Create(ctx context.Context, user domain.User, passwordHash string) error
	GetByID(ctx context.Context, id string) (domain.User, error)
	GetCredentials(ctx context.Context, userID string) (domain.Credentials, error)
	Search(ctx context.Context, firstNamePart string, secondNamePart string) ([]domain.User, error)
}

type SessionRepository interface {
	Create(ctx context.Context, token string, userID string) error
	GetUserID(ctx context.Context, token string) (string, error)
}

type FriendRepository interface {
	Add(ctx context.Context, userID string, friendID string) error
	Delete(ctx context.Context, userID string, friendID string) error
}

type PostRepository interface {
	CreateWithOutbox(ctx context.Context, post domain.Post, event domain.OutboxEvent) error
	GetByID(ctx context.Context, postID string) (domain.Post, error)
	GetByIDs(ctx context.Context, ids []string) ([]domain.Post, error)
	Feed(ctx context.Context, userID string, offset int, limit int) ([]domain.Post, error)
	UpdateTextWithOutbox(
		ctx context.Context,
		postID string,
		authorID string,
		text string,
		event domain.OutboxEvent,
	) error
	DeleteWithOutbox(ctx context.Context, postID string, authorID string, event domain.OutboxEvent) error
}

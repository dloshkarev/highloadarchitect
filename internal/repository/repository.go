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
	Create(ctx context.Context, post domain.Post) error
	GetByID(ctx context.Context, postID string) (domain.Post, error)
	Feed(ctx context.Context, userID string, offset int, limit int) ([]domain.Post, error)
	UpdateText(ctx context.Context, postID string, authorID string, text string) error
	Delete(ctx context.Context, postID string, authorID string) error
}

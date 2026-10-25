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
}

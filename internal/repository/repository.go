package repository

import (
	"context"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

type UserRepository interface {
	Create(ctx context.Context, user domain.User) error
	GetByID(ctx context.Context, id string) (domain.User, error)
}

type SessionRepository interface {
	Create(ctx context.Context, token string, userID string) error
}

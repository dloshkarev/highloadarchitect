package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
	"github.com/dloshkarev/highloadarchitect/internal/repository"
)

type AuthService struct {
	users    repository.UserRepository
	sessions repository.SessionRepository
}

func NewAuthService(users repository.UserRepository, sessions repository.SessionRepository) *AuthService {
	return &AuthService{users: users, sessions: sessions}
}

func (s *AuthService) Login(ctx context.Context, userID, password string) (string, error) {
	if password == "" {
		return "", domain.NewValidationError("пароль не заполнен")
	}
	if _, err := uuid.Parse(userID); err != nil {
		return "", domain.NewValidationError("идентификатор пользователя должен быть UUID")
	}

	credentials, err := s.users.GetCredentials(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("load credentials: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(credentials.PasswordHash), []byte(password)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return "", domain.NewValidationError("неверный пароль")
		}

		return "", fmt.Errorf("compare password: %w", err)
	}

	token := uuid.NewString()
	if err := s.sessions.Create(ctx, token, credentials.UserID); err != nil {
		return "", fmt.Errorf("save session: %w", err)
	}

	return token, nil
}

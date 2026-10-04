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
		return "", domain.ErrInvalidInput
	}
	if _, err := uuid.Parse(userID); err != nil {
		return "", domain.ErrInvalidInput
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("get user: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return "", domain.ErrInvalidInput
		}

		return "", fmt.Errorf("compare password: %w", err)
	}

	token := uuid.NewString()
	if err := s.sessions.Create(ctx, token, user.ID); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	return token, nil
}

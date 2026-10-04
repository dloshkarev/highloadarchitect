package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
	"github.com/dloshkarev/highloadarchitect/internal/repository"
)

type RegisterInput struct {
	FirstName  string
	SecondName string
	Birthdate  string
	Gender     string
	Biography  string
	City       string
	Password   string
}

type UserService struct {
	users repository.UserRepository
}

func NewUserService(users repository.UserRepository) *UserService {
	return &UserService{users: users}
}

func (s *UserService) Register(ctx context.Context, input RegisterInput) (string, error) {
	user, err := newUser(input)
	if err != nil {
		return "", err
	}
	if err := s.users.Create(ctx, user); err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}

	return user.ID, nil
}

func (s *UserService) Get(ctx context.Context, userID string) (domain.User, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return domain.User{}, domain.ErrInvalidInput
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return domain.User{}, fmt.Errorf("get user: %w", err)
	}

	return user, nil
}

func newUser(input RegisterInput) (domain.User, error) {
	firstName := strings.TrimSpace(input.FirstName)
	secondName := strings.TrimSpace(input.SecondName)
	if firstName == "" || secondName == "" || input.Password == "" {
		return domain.User{}, domain.ErrInvalidInput
	}
	if len(input.Password) > 72 || strings.ContainsRune(input.Password, 0) {
		return domain.User{}, domain.ErrInvalidInput
	}

	birthdate, err := time.Parse(time.DateOnly, strings.TrimSpace(input.Birthdate))
	if err != nil {
		return domain.User{}, domain.ErrInvalidInput
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, fmt.Errorf("hash password: %w", err)
	}

	return domain.User{
		ID:           uuid.NewString(),
		FirstName:    firstName,
		SecondName:   secondName,
		Birthdate:    birthdate,
		Gender:       strings.TrimSpace(input.Gender),
		Biography:    strings.TrimSpace(input.Biography),
		City:         strings.TrimSpace(input.City),
		PasswordHash: string(hash),
	}, nil
}

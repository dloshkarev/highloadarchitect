package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
	"github.com/dloshkarev/highloadarchitect/internal/repository"
)

const (
	maxNameLength      = 100
	maxCityLength      = 100
	maxBiographyLength = 2000
	minPasswordLength  = 8
	maxPasswordLength  = 72
)

type RegisterInput struct {
	FirstName  string
	SecondName string
	Birthdate  string
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
	user, passwordHash, err := newUser(input)
	if err != nil {
		return "", err
	}
	if err := s.users.Create(ctx, user, passwordHash); err != nil {
		return "", fmt.Errorf("register user: %w", err)
	}

	return user.ID, nil
}

func (s *UserService) Get(ctx context.Context, userID string) (domain.User, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return domain.User{}, domain.ErrInvalidInput
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return domain.User{}, fmt.Errorf("load user: %w", err)
	}

	return user, nil
}

func newUser(input RegisterInput) (domain.User, string, error) {
	input.FirstName = strings.TrimSpace(input.FirstName)
	input.SecondName = strings.TrimSpace(input.SecondName)
	input.Biography = strings.TrimSpace(input.Biography)
	input.City = strings.TrimSpace(input.City)
	if err := validateRegisterInput(input); err != nil {
		return domain.User{}, "", err
	}

	birthdate, err := time.Parse(time.DateOnly, strings.TrimSpace(input.Birthdate))
	if err != nil {
		return domain.User{}, "", domain.ErrInvalidInput
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, "", fmt.Errorf("hash password: %w", err)
	}

	return domain.User{
		ID:         uuid.NewString(),
		FirstName:  input.FirstName,
		SecondName: input.SecondName,
		Birthdate:  birthdate,
		Biography:  input.Biography,
		City:       input.City,
	}, string(hash), nil
}

func validateRegisterInput(input RegisterInput) error {
	if input.FirstName == "" || input.SecondName == "" {
		return domain.ErrInvalidInput
	}
	firstNameLen := utf8.RuneCountInString(input.FirstName)
	secondNameLen := utf8.RuneCountInString(input.SecondName)
	if firstNameLen > maxNameLength || secondNameLen > maxNameLength {
		return domain.ErrInvalidInput
	}
	cityLen := utf8.RuneCountInString(input.City)
	biographyLen := utf8.RuneCountInString(input.Biography)
	if cityLen > maxCityLength || biographyLen > maxBiographyLength {
		return domain.ErrInvalidInput
	}
	passwordTooShort := len(input.Password) < minPasswordLength
	passwordTooLong := len(input.Password) > maxPasswordLength
	if passwordTooShort || passwordTooLong || strings.ContainsRune(input.Password, 0) {
		return domain.ErrInvalidInput
	}

	return nil
}

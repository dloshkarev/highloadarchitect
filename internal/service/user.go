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
	maxNameLength          = 100
	maxCityLength          = 100
	maxBiographyLength     = 2000
	minPasswordLength      = 8
	maxPasswordLength      = 72
	bcryptMaxPasswordBytes = 72
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
		return domain.User{}, domain.NewValidationError("идентификатор пользователя должен быть UUID")
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
		return domain.User{}, "", domain.NewValidationError("дата рождения должна быть в формате ГГГГ-ММ-ДД")
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
	if input.FirstName == "" {
		return domain.NewValidationError("имя не заполнено")
	}
	if input.SecondName == "" {
		return domain.NewValidationError("фамилия не заполнена")
	}
	if utf8.RuneCountInString(input.FirstName) > maxNameLength {
		return domain.NewValidationError(fmt.Sprintf("имя длиннее %d символов", maxNameLength))
	}
	if utf8.RuneCountInString(input.SecondName) > maxNameLength {
		return domain.NewValidationError(fmt.Sprintf("фамилия длиннее %d символов", maxNameLength))
	}
	if utf8.RuneCountInString(input.City) > maxCityLength {
		return domain.NewValidationError(fmt.Sprintf("город длиннее %d символов", maxCityLength))
	}
	if utf8.RuneCountInString(input.Biography) > maxBiographyLength {
		return domain.NewValidationError(fmt.Sprintf("биография длиннее %d символов", maxBiographyLength))
	}
	passwordLength := utf8.RuneCountInString(input.Password)
	if passwordLength < minPasswordLength {
		return domain.NewValidationError(fmt.Sprintf("пароль короче %d символов", minPasswordLength))
	}
	if passwordLength > maxPasswordLength {
		return domain.NewValidationError(fmt.Sprintf("пароль длиннее %d символов", maxPasswordLength))
	}
	if len(input.Password) > bcryptMaxPasswordBytes {
		return domain.NewValidationError("пароль слишком длинный для сохранения")
	}
	if strings.ContainsRune(input.Password, 0) {
		return domain.NewValidationError("пароль содержит нулевой символ")
	}

	return nil
}

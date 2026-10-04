package domain

import "errors"

var (
	ErrNotFound      = errors.New("пользователь не найден")
	ErrRouteNotFound = errors.New("маршрут не найден")
)

type ValidationError struct {
	message string
}

func NewValidationError(message string) error {
	return &ValidationError{message: message}
}

func (e *ValidationError) Error() string {
	return e.message
}

type DBError struct {
	Err error
}

func (e *DBError) Error() string {
	return e.Err.Error()
}

func (e *DBError) Unwrap() error {
	return e.Err
}

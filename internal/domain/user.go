package domain

import "time"

type User struct {
	ID         string
	FirstName  string
	SecondName string
	Birthdate  time.Time
	Biography  string
	City       string
}

type Credentials struct {
	UserID       string
	PasswordHash string
}

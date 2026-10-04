package domain

import "time"

type User struct {
	ID           string
	FirstName    string
	SecondName   string
	Birthdate    time.Time
	Gender       string
	Biography    string
	City         string
	PasswordHash string
}

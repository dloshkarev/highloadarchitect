package domain

import "time"

type Post struct {
	ID        string
	AuthorID  string
	Text      string
	CreatedAt time.Time
}

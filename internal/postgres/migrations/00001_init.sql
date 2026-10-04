-- +goose Up
CREATE TABLE users (
    id UUID PRIMARY KEY,
    first_name TEXT NOT NULL,
    second_name TEXT NOT NULL,
    birthdate DATE NOT NULL,
    biography TEXT NOT NULL DEFAULT '',
    city TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL
);

CREATE TABLE sessions (
    token UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE sessions;
DROP TABLE users;

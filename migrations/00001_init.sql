-- +goose Up
-- Схема создаётся только миграциями; init-скриптов в build/dev больше нет.
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    first_name TEXT NOT NULL,
    second_name TEXT NOT NULL,
    birthdate DATE NOT NULL,
    biography TEXT NOT NULL DEFAULT '',
    city TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL DEFAULT '$2a$10$wPU0QusFYid9TqTV5jK21uwTOFHOc5iEvdUz7BGmZh6/Txl9aXsaq'
);

CREATE TABLE IF NOT EXISTS sessions (
    token UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE sessions;
DROP TABLE users;

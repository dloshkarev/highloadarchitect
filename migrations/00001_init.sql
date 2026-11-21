-- +goose Up
-- Схема создаётся только миграциями; init-скриптов в build/dev больше нет.
-- Дефолт password_hash — пароль затравочных пользователей из 00003
-- (people.v2.csv): 9se3gwWE3Y9j4VU2. Дефолт нужен на время наполнения,
-- 00003 в конце снимает его, чтобы реальные регистрации хешировали свой пароль.
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    first_name TEXT NOT NULL,
    second_name TEXT NOT NULL,
    birthdate DATE NOT NULL,
    biography TEXT NOT NULL DEFAULT '',
    city TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL DEFAULT '$2a$10$W0HrXoeOxu7ocolAw6hvqOD/0Xy/3ztFrSmQx8AEUCEWdtqGctP0C'
);

CREATE TABLE IF NOT EXISTS sessions (
    token UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE sessions;
DROP TABLE users;

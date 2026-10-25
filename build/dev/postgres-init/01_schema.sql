-- Локальная схема users. Это не goose-миграция.
-- Файл монтируется в docker-entrypoint-initdb.d и выполняется только когда
-- docker compose впервые создаёт том Postgres.
-- Значения по умолчанию у id и password_hash нужны для 02_users_data.sql
-- и снимаются в конце наполнения.

SET client_encoding = 'UTF8';

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    first_name TEXT NOT NULL,
    second_name TEXT NOT NULL,
    birthdate DATE NOT NULL,
    biography TEXT NOT NULL DEFAULT '',
    city TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL DEFAULT '$2a$10$wPU0QusFYid9TqTV5jK21uwTOFHOc5iEvdUz7BGmZh6/Txl9aXsaq'
);

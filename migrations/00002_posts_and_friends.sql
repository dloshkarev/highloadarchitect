-- +goose Up
CREATE TABLE IF NOT EXISTS posts (
    id UUID PRIMARY KEY,
    author_id UUID NOT NULL REFERENCES users (id),
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS friends (
    user_id UUID NOT NULL REFERENCES users (id),
    friend_id UUID NOT NULL REFERENCES users (id),
    PRIMARY KEY (user_id, friend_id),
    CONSTRAINT friends_no_self CHECK (user_id <> friend_id)
);

-- +goose Down
DROP TABLE posts;
DROP TABLE friends;

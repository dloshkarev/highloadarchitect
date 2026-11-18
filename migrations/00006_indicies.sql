-- +goose Up
CREATE INDEX IF NOT EXISTS idx_users_search
    ON users (
    first_name text_pattern_ops,
    second_name text_pattern_ops
);

CREATE INDEX IF NOT EXISTS idx_posts_author_created_at
    ON posts (author_id, created_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_users_search;
DROP INDEX IF EXISTS idx_posts_author_created_at;

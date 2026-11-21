-- +goose Up
-- Поиск всех пользователей, которые добавили автора в друзья.
CREATE INDEX IF NOT EXISTS idx_friends_friend_user
    ON friends (friend_id, user_id);

-- +goose Down
DROP INDEX IF EXISTS idx_friends_friend_user;

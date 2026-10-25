-- Индексы. Накладываются после наполнения БД

-- hw2
CREATE INDEX IF NOT EXISTS idx_users_search
ON users (
    first_name text_pattern_ops,
    second_name text_pattern_ops
);

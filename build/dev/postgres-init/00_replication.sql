-- Роль для потоковой репликации. Выполняется первой (00_) при инициализации
-- пустого тома Postgres, до схемы и наполнения данными.

CREATE ROLE replicator WITH LOGIN REPLICATION PASSWORD 'replicator';

#!/usr/bin/env bash
# Выполняется Patroni один раз после initdb лидера: создаёт роль и базу
# приложения и накатывает схему с наполнением (аналог docker-entrypoint-initdb.d).
# Patroni передаёт строку подключения к временному инстансу первым аргументом.
set -euo pipefail

CONNINFO="${1:?ожидалась строка подключения от patroni}"

psql -v ON_ERROR_STOP=1 "$CONNINFO" -c "CREATE ROLE social WITH LOGIN PASSWORD 'social';"
psql -v ON_ERROR_STOP=1 "$CONNINFO" -c "CREATE DATABASE social OWNER social;"

# Сиды накатываем от роли приложения, чтобы владельцем таблиц была она,
# как при docker-entrypoint-initdb.d (иначе миграции приложения упираются в права).
for script in /seed/*.sql; do
	echo "post_bootstrap: накатываем $script"
	psql -v ON_ERROR_STOP=1 "$CONNINFO user=social dbname=social" -f "$script"
done

echo "post_bootstrap: готово"

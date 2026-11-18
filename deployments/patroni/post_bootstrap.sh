#!/usr/bin/env bash
# Выполняется Patroni один раз после initdb лидера: создаёт роль и базу
# приложения. Схему и наполнение накатывают goose-миграции при старте приложения.
# Patroni передаёт строку подключения к временному инстансу первым аргументом.
set -euo pipefail

CONNINFO="${1:?ожидалась строка подключения от patroni}"

psql -v ON_ERROR_STOP=1 "$CONNINFO" -c "CREATE ROLE social WITH LOGIN PASSWORD 'social';"
psql -v ON_ERROR_STOP=1 "$CONNINFO" -c "CREATE DATABASE social OWNER social;"

echo "post_bootstrap: готово"

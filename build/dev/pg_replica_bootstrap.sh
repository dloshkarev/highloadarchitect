#!/usr/bin/env bash
# Bootstrap реплики PostgreSQL: на пустой PGDATA клонирует мастер через
# pg_basebackup (флаг -R сам пишет standby.signal и primary_conninfo),
# после чего управление передаётся штатному entrypoint образа postgres.
set -euo pipefail

: "${PGDATA:=/var/lib/postgresql/data}"
: "${PGMASTER_HOST:=pgmaster}"
: "${PGMASTER_PORT:=5432}"
: "${REPLICATION_USER:=replicator}"
: "${REPLICATION_PASSWORD:?задайте REPLICATION_PASSWORD}"
: "${REPLICA_NAME:?задайте REPLICA_NAME}"

if [ ! -s "$PGDATA/PG_VERSION" ]; then
	echo "PGDATA пустая: ждём мастер $PGMASTER_HOST:$PGMASTER_PORT"
	until pg_isready -q -h "$PGMASTER_HOST" -p "$PGMASTER_PORT" -U "$REPLICATION_USER" -d postgres; do
		sleep 1
	done

	echo "клонируем мастер через pg_basebackup"
	mkdir -p "$PGDATA"
	chown postgres:postgres "$PGDATA"
	chmod 700 "$PGDATA"
	# Копирование большого кластера может отставать от ротации WAL на мастере — повторяем попытки.
	for attempt in 1 2 3; do
		if gosu postgres pg_basebackup -R \
			-d "host=$PGMASTER_HOST port=$PGMASTER_PORT user=$REPLICATION_USER password=$REPLICATION_PASSWORD application_name=$REPLICA_NAME" \
			-D "$PGDATA" -v -P --wal-method=stream; then
			break
		fi
		echo "pg_basebackup не удался (попытка $attempt), повтор через 5 секунд"
		sleep 5
	done
	[ -s "$PGDATA/PG_VERSION" ] || { echo "pg_basebackup: не удалось клонировать мастер"; exit 1; }
	echo "клонирование завершено, реплика $REPLICA_NAME готова к старту"
fi

exec docker-entrypoint.sh "$@"

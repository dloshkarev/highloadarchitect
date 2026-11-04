#!/usr/bin/env bash
# Точка входа patroni-узла: чинит права на volume с данными
# и запускает patroni от пользователя postgres.
set -e

mkdir -p /var/lib/postgresql/data
chown -R postgres:postgres /var/lib/postgresql/data
chmod 700 /var/lib/postgresql/data

exec gosu postgres patroni "$@"

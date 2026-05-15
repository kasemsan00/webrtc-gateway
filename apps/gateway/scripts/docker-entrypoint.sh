#!/bin/sh
set -eu

if [ "${DB_AUTO_MIGRATE:-false}" = "true" ]; then
  if [ -z "${DB_DSN:-}" ]; then
    echo "DB_AUTO_MIGRATE=true but DB_DSN is empty" >&2
    exit 1
  fi

  ./goose -dir ./migrations postgres "$DB_DSN" up
fi

exec "$@"

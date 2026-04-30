#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "Usage: ./apps/gateway/scripts/migrate.sh <status|up|up-by-one|down|down-to|redo|reset|version|create> [name_or_version]"
  exit 1
fi

COMMAND="$1"
ARG1="${2:-}"
DIR="${MIGRATIONS_DIR:-apps/gateway/migrations}"
GOOSE_VERSION="${GOOSE_VERSION:-v3.24.1}"
GOOSE_BIN="github.com/pressly/goose/v3/cmd/goose@${GOOSE_VERSION}"

if [[ "$COMMAND" == "create" ]]; then
  if [[ -z "$ARG1" ]]; then
    echo "Name is required for create. Example: ./apps/gateway/scripts/migrate.sh create add_new_column"
    exit 1
  fi
  go run "$GOOSE_BIN" -dir "$DIR" create "$ARG1" sql
  exit 0
fi

: "${DB_DSN:?DB_DSN is required for migration commands except create}"

case "$COMMAND" in
  status)
    go run "$GOOSE_BIN" -dir "$DIR" postgres "$DB_DSN" status
    ;;
  up)
    go run "$GOOSE_BIN" -dir "$DIR" postgres "$DB_DSN" up
    ;;
  up-by-one)
    go run "$GOOSE_BIN" -dir "$DIR" postgres "$DB_DSN" up-by-one
    ;;
  down)
    go run "$GOOSE_BIN" -dir "$DIR" postgres "$DB_DSN" down
    ;;
  down-to)
    if [[ -z "$ARG1" ]]; then
      echo "Version is required for down-to. Example: ./apps/gateway/scripts/migrate.sh down-to 202603170001"
      exit 1
    fi
    go run "$GOOSE_BIN" -dir "$DIR" postgres "$DB_DSN" down-to "$ARG1"
    ;;
  redo)
    go run "$GOOSE_BIN" -dir "$DIR" postgres "$DB_DSN" redo
    ;;
  reset)
    go run "$GOOSE_BIN" -dir "$DIR" postgres "$DB_DSN" reset
    ;;
  version)
    go run "$GOOSE_BIN" -dir "$DIR" postgres "$DB_DSN" version
    ;;
  *)
    echo "Unknown command: $COMMAND"
    exit 1
    ;;
esac

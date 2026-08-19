#!/bin/sh
set -eu

# Named volumes such as gateway_logs:/app/gateway/logs are created as root.
# Take ownership, then drop to k2 before bootstrap or gateway start.
if [ "$(id -u)" = "0" ]; then
  mkdir -p logs
  chown -R k2:k2 logs
  exec su-exec k2 "$0" "$@"
fi

if [ "${DB_AUTO_MIGRATE:-false}" = "true" ] && [ "${DB_BOOTSTRAP_ON_START+x}" != "x" ]; then
  echo "DB_AUTO_MIGRATE is deprecated; use DB_BOOTSTRAP_ON_START=true for a single-instance deployment" >&2
  DB_BOOTSTRAP_ON_START=true
fi

if [ "${DB_BOOTSTRAP_ON_START:-false}" = "true" ]; then
  if [ -z "${DB_DSN:-}" ]; then
    echo "DB_BOOTSTRAP_ON_START=true but DB_DSN is empty" >&2
    exit 1
  fi

  ./db-bootstrap
fi

exec "$@"

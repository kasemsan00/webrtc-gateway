#!/bin/sh
set -eu

STACK_PROXY_PORT="${STACK_PROXY_PORT:-8088}"
GATEWAY_PORT="${API_PORT:-8080}"
FRONTEND_PORT="${FRONTEND_PORT:-4173}"
PROXY_PORT="${STACK_PROXY_PORT}"
export PROXY_PORT GATEWAY_PORT FRONTEND_PORT

gateway_pid=""
frontend_pid=""

shutdown() {
  echo "Shutting down k2-stack..."
  if [ -n "$frontend_pid" ]; then kill "$frontend_pid" 2>/dev/null || true; fi
  if [ -n "$gateway_pid" ]; then kill "$gateway_pid" 2>/dev/null || true; fi
}

trap shutdown INT TERM

cd /app/gateway
su-exec k2 ./docker-entrypoint.sh ./k2-gateway &
gateway_pid=$!

cd /app/frontend
export PORT="${FRONTEND_PORT}"
export HOST="${HOST:-0.0.0.0}"
export VITE_BASE_PATH="${VITE_BASE_PATH:-/admin/}"
node docker-server.mjs &
frontend_pid=$!

sleep 2

envsubst '${PROXY_PORT} ${GATEWAY_PORT} ${FRONTEND_PORT}' \
  < /etc/nginx/nginx.conf.template \
  > /etc/nginx/nginx.conf

echo "k2-stack listening on :${STACK_PROXY_PORT} (gateway :${GATEWAY_PORT}, frontend :${FRONTEND_PORT})"

exec nginx -g 'daemon off;'

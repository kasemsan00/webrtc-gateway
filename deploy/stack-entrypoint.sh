#!/bin/sh
set -eu

STACK_PROXY_PORT="${STACK_PROXY_PORT:-8088}"
GATEWAY_PORT="${API_PORT:-8080}"
FRONTEND_PORT="${FRONTEND_PORT:-4173}"
PROXY_PORT="${STACK_PROXY_PORT}"
export PROXY_PORT GATEWAY_PORT FRONTEND_PORT

gateway_pid=""
frontend_pid=""
nginx_pid=""

shutdown() {
  echo "Shutting down webrtc-sip-gateway-stack..."
  if [ -n "$nginx_pid" ]; then kill "$nginx_pid" 2>/dev/null || true; fi
  if [ -n "$frontend_pid" ]; then kill "$frontend_pid" 2>/dev/null || true; fi
  if [ -n "$gateway_pid" ]; then kill "$gateway_pid" 2>/dev/null || true; fi
}

trap shutdown INT TERM

cd /app/gateway
# Run as root so docker-entrypoint.sh can chown a mounted logs volume, then su-exec gateway.
./docker-entrypoint.sh ./webrtc-sip-gateway &
gateway_pid=$!

cd /app/frontend
export PORT="${FRONTEND_PORT}"
export HOST="${HOST:-0.0.0.0}"
export VITE_BASE_PATH="${VITE_BASE_PATH:-/admin/}"
node docker-server.mjs &
frontend_pid=$!

sleep 2

if ! kill -0 "$gateway_pid" 2>/dev/null; then
  echo "Gateway exited during startup" >&2
  exit 1
fi
if ! kill -0 "$frontend_pid" 2>/dev/null; then
  echo "Frontend exited during startup" >&2
  shutdown
  exit 1
fi

envsubst '${PROXY_PORT} ${GATEWAY_PORT} ${FRONTEND_PORT}' \
  < /etc/nginx/nginx.conf.template \
  > /etc/nginx/nginx.conf

echo "webrtc-sip-gateway-stack listening on :${STACK_PROXY_PORT} (gateway :${GATEWAY_PORT}, frontend :${FRONTEND_PORT})"

nginx -g 'daemon off;' &
nginx_pid=$!

while kill -0 "$gateway_pid" 2>/dev/null && kill -0 "$frontend_pid" 2>/dev/null && kill -0 "$nginx_pid" 2>/dev/null; do
  sleep 1
done

echo "A required webrtc-sip-gateway-stack process exited unexpectedly" >&2
shutdown
wait "$gateway_pid" "$frontend_pid" "$nginx_pid" 2>/dev/null || true
exit 1

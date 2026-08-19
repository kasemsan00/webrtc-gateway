#!/bin/sh
# Integration smoke test for a released gateway image. Requires Docker.
set -eu

IMAGE="${DB_BOOTSTRAP_TEST_IMAGE:-k2-gateway-bootstrap-test}"
NETWORK="k2-bootstrap-it-$$"
POSTGRES="${NETWORK}-postgres"
DSN="postgres://k2:k2pass@${POSTGRES}:5432/k2?sslmode=disable"

cleanup() {
  docker rm -f "$POSTGRES" >/dev/null 2>&1 || true
  docker network rm "$NETWORK" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

docker network create "$NETWORK" >/dev/null
docker run -d --name "$POSTGRES" --network "$NETWORK" \
  -e POSTGRES_DB=k2 -e POSTGRES_USER=k2 -e POSTGRES_PASSWORD=k2pass \
  postgres:18-alpine >/dev/null

attempt=0
until docker exec "$POSTGRES" pg_isready -U k2 -d k2 >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    echo "PostgreSQL did not become ready" >&2
    exit 1
  fi
  sleep 1
done

# Two fresh runners must serialize via the database advisory lock.
docker run --rm --network "$NETWORK" -e "DB_DSN=$DSN" --entrypoint ./db-bootstrap "$IMAGE" &
first_pid=$!
docker run --rm --network "$NETWORK" -e "DB_DSN=$DSN" --entrypoint ./db-bootstrap "$IMAGE" &
second_pid=$!
wait "$first_pid"
wait "$second_pid"

# A fully migrated schema is a no-op on subsequent runs.
docker run --rm --network "$NETWORK" -e "DB_DSN=$DSN" --entrypoint ./db-bootstrap "$IMAGE"
docker exec "$POSTGRES" psql -U k2 -d k2 -v ON_ERROR_STOP=1 -c \
  "SELECT to_regclass('public.sip_trunks'), max(version_id) FROM goose_db_version WHERE is_applied"

# A schema created by the legacy init SQL but without Goose history is adopted
# without recreating tables or losing representative data.
docker exec "$POSTGRES" createdb -U k2 legacy
docker cp "$(dirname "$0")/../schema/bootstrap-baseline.sql" "$POSTGRES:/tmp/bootstrap-baseline.sql"
docker exec "$POSTGRES" psql -U k2 -d legacy -v ON_ERROR_STOP=1 -f /tmp/bootstrap-baseline.sql
docker exec "$POSTGRES" psql -U k2 -d legacy -v ON_ERROR_STOP=1 -c \
  "INSERT INTO sip_trunks (name, domain, username, password) VALUES ('preserved-trunk', 'example.test', 'user', 'secret')"
docker run --rm --network "$NETWORK" -e "DB_DSN=postgres://k2:k2pass@${POSTGRES}:5432/legacy?sslmode=disable" --entrypoint ./db-bootstrap "$IMAGE"
docker exec "$POSTGRES" psql -U k2 -d legacy -v ON_ERROR_STOP=1 -c \
  "SELECT name FROM sip_trunks WHERE name = 'preserved-trunk'"

# A damaged managed database must be rejected, not automatically repaired.
docker exec "$POSTGRES" psql -U k2 -d k2 -v ON_ERROR_STOP=1 -c "DROP TABLE call_stats"
if docker run --rm --network "$NETWORK" -e "DB_DSN=$DSN" --entrypoint ./db-bootstrap "$IMAGE"; then
  echo "bootstrap unexpectedly accepted an inconsistent schema" >&2
  exit 1
fi

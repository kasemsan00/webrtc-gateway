# Scripts Usage

This folder contains helper scripts for database migrations, container startup, and bootstrap integration testing.

## 1. `migrate.ps1`

PowerShell wrapper for Goose migrations (`./apps/gateway/scripts/migrate.ps1`).

### Requirements & Parameters

- PowerShell and Go installed — runs `go run github.com/pressly/goose/v3/cmd/goose@$GooseVersion` so Goose is downloaded/executed automatically (no pre-installed `goose` binary needed).
- `-Command` (required, positional 0): `status`, `up`, `up-by-one`, `down`, `down-to`, `redo`, `reset`, `version`, `create`.
- `-Name` (positional 1): migration name for `create` or target version for `down-to`.
- `-Dsn`: PostgreSQL DSN (defaults to `$env:DB_DSN`; required for all commands except `create`).
- `-Dir`: migrations directory (optional; defaults to `../migrations` relative to the script).
- `-GooseVersion`: Goose module version (optional; defaults to `v3.24.1`).

### Examples

Run from repo root:

```powershell
$env:DB_DSN="postgres://gateway_user:gateway_dev_password@localhost:5432/webrtc_sip_gateway?sslmode=disable"
./apps/gateway/scripts/migrate.ps1 status
./apps/gateway/scripts/migrate.ps1 up
```

Pass `-Dsn` and `-GooseVersion` explicitly:

```powershell
./apps/gateway/scripts/migrate.ps1 up -Dsn "postgres://gateway_user:gateway_dev_password@localhost:5432/webrtc_sip_gateway?sslmode=disable" -GooseVersion v3.24.1
```

Create or roll back migrations:

```powershell
./apps/gateway/scripts/migrate.ps1 create add_new_column
./apps/gateway/scripts/migrate.ps1 down-to 202603170001
```

## 2. `migrate.sh`

Bash equivalent of `migrate.ps1` (`./apps/gateway/scripts/migrate.sh`).

### Environment Variables & Arguments

- Positional arguments: `<status|up|up-by-one|down|down-to|redo|reset|version|create> [name_or_version]`.
- `DB_DSN`: PostgreSQL DSN (required for all commands except `create`).
- `MIGRATIONS_DIR`: migrations directory (optional; defaults to `../migrations` relative to the script).
- `GOOSE_VERSION`: Goose module version (optional; defaults to `v3.24.1`; executed via `go run github.com/pressly/goose/v3/cmd/goose@${GOOSE_VERSION}`).

### Examples

```bash
export DB_DSN="postgres://gateway_user:gateway_dev_password@localhost:5432/webrtc_sip_gateway?sslmode=disable"
./apps/gateway/scripts/migrate.sh status
./apps/gateway/scripts/migrate.sh up
./apps/gateway/scripts/migrate.sh create add_new_column
./apps/gateway/scripts/migrate.sh down-to 202603170001
```

## 3. `docker-entrypoint.sh`

Container entrypoint used by `apps/gateway/Dockerfile` and invoked inside `deploy/stack-entrypoint.sh`:

1. When started as `root`, creates `logs` and `/var/lib/webrtc-gateway/chat-images` if needed, `chown`s them to `gateway:gateway`, and drops privileges via `su-exec gateway`.
2. Supports single-instance/local startup bootstrap (used when `DB_ENABLE=true`): if `DB_BOOTSTRAP_ON_START=true` (or deprecated `DB_AUTO_MIGRATE=true` when `DB_BOOTSTRAP_ON_START` is unset), verifies `DB_DSN` is non-empty and runs `./db-bootstrap` before `exec "$@"`.
3. For multi-instance production deployments, keep `DB_BOOTSTRAP_ON_START=false` on application containers and run `./db-bootstrap` as a dedicated one-shot deploy job instead.

## 4. `test-db-bootstrap.sh`

Docker integration smoke test (`./apps/gateway/scripts/test-db-bootstrap.sh`) for `./db-bootstrap` inside a built gateway image:

- Reads `DB_BOOTSTRAP_TEST_IMAGE` (defaults to `webrtc-sip-gateway-bootstrap-test`).
- Starts a temporary `postgres:18-alpine` container on an isolated Docker network and verifies:
  1. Two concurrent `./db-bootstrap` runs on a fresh database serialize cleanly via the PostgreSQL advisory lock.
  2. Re-running `./db-bootstrap` on an already migrated database is a no-op.
  3. A `legacy-unversioned` schema (`schema/bootstrap-baseline.sql` without Goose history) is adopted without losing existing rows in `sip_trunks`.
  4. A damaged (`inconsistent`) managed schema (e.g. dropped `call_stats`) is rejected with a non-zero exit code rather than silently repaired.

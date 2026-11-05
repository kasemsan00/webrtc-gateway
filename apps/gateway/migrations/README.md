# Gateway DB Migrations (Goose)

This project uses Goose (`apps/gateway/migrations`) together with the automated bootstrap/migration runner `cmd/db-bootstrap` (`internal/dbbootstrap`) and baseline schema `schema/bootstrap-baseline.sql`.

## Automated bootstrap (`db-bootstrap`)

`cmd/db-bootstrap` (`internal/dbbootstrap`) coordinates schema initialization and Goose migrations:

1. Connects via `DB_DSN` and acquires a PostgreSQL advisory lock (`0x4b325f44425f4253`) so concurrent runners serialize cleanly.
2. Classifies the schema into one of six states:
   - `fresh` — no core tables and no `goose_db_version`; installs `schema/bootstrap-baseline.sql` in a transaction, then runs `goose up`.
   - `legacy-unversioned` — all core tables and critical columns already exist without `goose_db_version`; adopts the existing tables without recreating them, then runs `goose up`.
   - `managed` — all core tables, critical columns, and `goose_db_version` exist; verifies migration history and runs `goose up`.
   - `partial` — some core tables/columns exist without `goose_db_version`; refuses automatic repair and exits non-zero.
   - `inconsistent` — `goose_db_version` exists but core tables or critical columns are missing; refuses automatic repair and exits non-zero.
   - `image-too-old` — `goose_db_version` contains applied versions missing from the image's `migrations/` directory; aborts safely so an older image cannot run against a newer schema.
3. Optional environment overrides: `DB_BOOTSTRAP_BASELINE` (default `./schema/bootstrap-baseline.sql`), `DB_BOOTSTRAP_MIGRATIONS_DIR` (default `./migrations`), `DB_BOOTSTRAP_GOOSE_BINARY` (default `./goose`), `DB_BOOTSTRAP_LOCK_TIMEOUT` (default `30s`), and `DB_BOOTSTRAP_MIGRATION_TIMEOUT` (default `5m`).

> **Warning:** `init.sql` and `schema/bootstrap-baseline.sql` alone omit `auth_mode`, `trunk_id`, `trunk_name`, and `sip_username` on `call_sessions` until Goose migration `202602160001` (`202602160001_add_auth_context_to_call_sessions.sql`) runs. Running `db-bootstrap` or `migrate.ps1` / `migrate.sh up` applies that migration automatically — never stop after manually loading `init.sql` or `schema/bootstrap-baseline.sql`.

## Versioning rule

Use a sortable timestamp prefix with enough precision to avoid collisions.

Example format:

- `202603310001_add_notify_user_id_to_sip_trunks.sql`
- `202605270001_add_last_online_platform_to_sip_trunks.sql`

## File format

Each migration must include both sections:

```sql
-- +goose Up
-- migration statements

-- +goose Down
-- rollback statements
```

Idempotent ensure/repair-guard migrations (for example `202605140002_ensure_push_contact_columns_on_sip_trunks.sql`) may use a no-op `SELECT 1;` in `-- +goose Down` when an earlier migration owns rollback for those columns.

## Run migrations manually

PowerShell:

```powershell
$env:DB_DSN = "postgres://gateway_user:gateway_dev_password@localhost:5432/webrtc_sip_gateway?sslmode=disable"
./apps/gateway/scripts/migrate.ps1 status
./apps/gateway/scripts/migrate.ps1 up
```

Bash:

```bash
export DB_DSN="postgres://gateway_user:gateway_dev_password@localhost:5432/webrtc_sip_gateway?sslmode=disable"
./apps/gateway/scripts/migrate.sh status
./apps/gateway/scripts/migrate.sh up
```

Create a new migration:

```powershell
./apps/gateway/scripts/migrate.ps1 create add_new_column
```

```bash
./apps/gateway/scripts/migrate.sh create add_new_column
```

## Multi-instance deploy rule

Run the image's `db-bootstrap` command once as a dedicated deploy step before rolling out gateway instances. It acquires a PostgreSQL advisory lock, installs the baseline schema for a fresh database, and applies pending Goose migrations.

Do not run schema migration from every gateway instance at startup. Production compose profiles provide a one-shot `db-bootstrap` service and gateway instances start only after it succeeds. Use `DB_BOOTSTRAP_ON_START=true` only for a local or explicitly single-instance deployment.

For a fresh PostgreSQL database, use the matching release image; it contains the canonical `schema/bootstrap-baseline.sql`, migrations, Goose binary, and bootstrap executable. Do not manually apply `init.sql` followed by an uncoordinated migration command.

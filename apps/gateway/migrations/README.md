# Gateway DB Migrations (Goose)

This project uses Goose as the migration runner for `apps/gateway/migrations`.

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

## Run migrations

PowerShell:

```powershell
$env:DB_DSN = "postgres://k2user:k2pass@localhost:5432/k2_gateway?sslmode=disable"
./apps/gateway/scripts/migrate.ps1 status
./apps/gateway/scripts/migrate.ps1 up
```

Bash:

```bash
export DB_DSN="postgres://k2user:k2pass@localhost:5432/k2_gateway?sslmode=disable"
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

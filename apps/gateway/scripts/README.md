# Scripts Usage

This folder contains helper scripts for database migrations.

## migrate.ps1

PowerShell wrapper for Goose migrations.

Path: `./apps/gateway/scripts/migrate.ps1`

### Requirements

- PowerShell
- Go installed (the script uses `go run` to execute Goose)
- Database DSN for migration commands (`status`, `up`, `down`, etc.)

### Commands

- `status`
- `up`
- `up-by-one`
- `down`
- `down-to <version>`
- `redo`
- `reset`
- `version`
- `create <name>`

### Examples

Run from repo root:

```powershell
$env:DB_DSN="postgres://k2user:k2pass@localhost:5432/k2_gateway?sslmode=disable"
./apps/gateway/scripts/migrate.ps1 status
./apps/gateway/scripts/migrate.ps1 up
```

Run from `apps/gateway`:

```powershell
$env:DB_DSN="postgres://k2user:k2pass@localhost:5432/k2_gateway?sslmode=disable"
./scripts/migrate.ps1 status
./scripts/migrate.ps1 up
```

Use DSN directly (without setting env var):

```powershell
./scripts/migrate.ps1 up -Dsn "postgres://k2user:k2pass@localhost:5432/k2_gateway?sslmode=disable"
```

Create a new migration:

```powershell
./scripts/migrate.ps1 create add_new_column
```

Rollback to a specific version:

```powershell
./scripts/migrate.ps1 down-to 202603170001
```

### Notes

- `-Dir` is optional. If omitted, the script resolves migrations directory automatically relative to the script location.
- `create` does not require `DB_DSN`.
- For production, run migrations as a dedicated deployment step before rolling update.

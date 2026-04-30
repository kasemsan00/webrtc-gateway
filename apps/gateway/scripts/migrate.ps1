param(
  [Parameter(Mandatory = $true, Position = 0)]
  [ValidateSet('status', 'up', 'up-by-one', 'down', 'down-to', 'redo', 'reset', 'version', 'create')]
  [string]$Command,

  [Parameter(Position = 1)]
  [string]$Name,

  [string]$Dsn = $env:DB_DSN,
  [string]$Dir,
  [string]$GooseVersion = 'v3.24.1'
)

$ErrorActionPreference = 'Stop'
$goose = "go run github.com/pressly/goose/v3/cmd/goose@$GooseVersion"

if ([string]::IsNullOrWhiteSpace($Dir)) {
  $Dir = Join-Path $PSScriptRoot '..\migrations'
}

$Dir = (Resolve-Path $Dir).Path

if ($Command -eq 'create') {
  if ([string]::IsNullOrWhiteSpace($Name)) {
    throw 'Name is required for create. Example: ./apps/gateway/scripts/migrate.ps1 create add_new_column'
  }

  & go run "github.com/pressly/goose/v3/cmd/goose@$GooseVersion" -dir $Dir create $Name sql
  exit $LASTEXITCODE
}

if ([string]::IsNullOrWhiteSpace($Dsn)) {
  throw 'DB_DSN is required for migration commands except create.'
}

switch ($Command) {
  'status' { & go run "github.com/pressly/goose/v3/cmd/goose@$GooseVersion" -dir $Dir postgres $Dsn status }
  'up' { & go run "github.com/pressly/goose/v3/cmd/goose@$GooseVersion" -dir $Dir postgres $Dsn up }
  'up-by-one' { & go run "github.com/pressly/goose/v3/cmd/goose@$GooseVersion" -dir $Dir postgres $Dsn up-by-one }
  'down' { & go run "github.com/pressly/goose/v3/cmd/goose@$GooseVersion" -dir $Dir postgres $Dsn down }
  'down-to' {
    if ([string]::IsNullOrWhiteSpace($Name)) {
      throw 'Version is required for down-to. Example: ./apps/gateway/scripts/migrate.ps1 down-to 202603170001'
    }
    & go run "github.com/pressly/goose/v3/cmd/goose@$GooseVersion" -dir $Dir postgres $Dsn down-to $Name
  }
  'redo' { & go run "github.com/pressly/goose/v3/cmd/goose@$GooseVersion" -dir $Dir postgres $Dsn redo }
  'reset' { & go run "github.com/pressly/goose/v3/cmd/goose@$GooseVersion" -dir $Dir postgres $Dsn reset }
  'version' { & go run "github.com/pressly/goose/v3/cmd/goose@$GooseVersion" -dir $Dir postgres $Dsn version }
}

exit $LASTEXITCODE

$branch = $env:BRANCH ?? "1.3.2"
$registry = $env:REGISTRY ?? "registry.kasemsan.com"

if ($env:RUN_MIGRATIONS -eq "true") {
  if (-not $env:DB_DSN) {
    throw "RUN_MIGRATIONS=true requires DB_DSN"
  }

  $migrationScript = Join-Path $PSScriptRoot "apps/gateway/scripts/migrate.ps1"
  & $migrationScript up -Dsn $env:DB_DSN
  if ($LASTEXITCODE -ne 0) {
    throw "Goose migration failed with exit code $LASTEXITCODE"
  }
}

$viteBasePath = if ($env:VITE_BASE_PATH) { $env:VITE_BASE_PATH } else { "/admin/" }

docker build --push `
  -t $registry/k2-frontend:$branch `
  --build-arg VITE_GATEWAY_URL=$env:VITE_GATEWAY_URL `
  --build-arg VITE_TURN_URL=$env:VITE_TURN_URL `
  --build-arg VITE_TURN_USERNAME=$env:VITE_TURN_USERNAME `
  --build-arg VITE_KEYCLOAK_URL=$env:VITE_KEYCLOAK_URL `
  --build-arg VITE_KEYCLOAK_REALM=$env:VITE_KEYCLOAK_REALM `
  --build-arg VITE_KEYCLOAK_CLIENT=$env:VITE_KEYCLOAK_CLIENT `
  --build-arg VITE_CONFIG_AUTORECORD=$env:VITE_CONFIG_AUTORECORD `
  --build-arg VITE_BASE_PATH=$viteBasePath `
  -f apps/frontend/Dockerfile .

docker build --push -t $registry/k2-gateway:$branch -f apps/gateway/Dockerfile .

docker build --push `
  -t $registry/k2-stack:$branch `
  --build-arg VITE_GATEWAY_URL=$env:VITE_GATEWAY_URL `
  --build-arg VITE_TURN_URL=$env:VITE_TURN_URL `
  --build-arg VITE_TURN_USERNAME=$env:VITE_TURN_USERNAME `
  --build-arg VITE_KEYCLOAK_URL=$env:VITE_KEYCLOAK_URL `
  --build-arg VITE_KEYCLOAK_REALM=$env:VITE_KEYCLOAK_REALM `
  --build-arg VITE_KEYCLOAK_CLIENT=$env:VITE_KEYCLOAK_CLIENT `
  --build-arg VITE_CONFIG_AUTORECORD=$env:VITE_CONFIG_AUTORECORD `
  --build-arg VITE_BASE_PATH=$viteBasePath `
  -f deploy/Dockerfile.unified .

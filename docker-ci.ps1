$branch = $env:BRANCH ?? "1.4.1"
$registry = $env:REGISTRY ?? "registry.kasemsan.com"
$platforms = $env:PLATFORMS ?? "linux/amd64,linux/arm64"
# $platforms = $env:PLATFORMS ?? "linux/amd64"

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

docker buildx build --push `
  --platform $platforms `
  -t $registry/k2-stack:$branch `
  --build-arg VITE_GATEWAY_URL=$env:VITE_GATEWAY_URL `
  --build-arg VITE_CONFIG_AUTORECORD=$env:VITE_CONFIG_AUTORECORD `
  --build-arg VITE_BASE_PATH=$viteBasePath `
  -f deploy/Dockerfile.unified .

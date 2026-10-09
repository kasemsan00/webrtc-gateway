$ErrorActionPreference = "Stop"

$branch = $env:BRANCH ?? "1.4.1"
$registry = $env:REGISTRY ?? "registry.kasemsan.com"
# $platforms = $env:PLATFORMS ?? "linux/amd64,linux/arm64"
$platforms = $env:PLATFORMS ?? "linux/amd64"

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
$tagLatest = $true
if (-not [string]::IsNullOrWhiteSpace($env:TAG_LATEST)) {
  $tagLatest = $env:TAG_LATEST.Trim() -notin @("0", "false", "False", "FALSE", "no")
}
# Publish the immutable release tag and, unless TAG_LATEST=false, the canonical
# latest tag from the same multi-platform build manifest.
$tags = @(
  "-t", "${registry}/webrtc-sip-gateway-stack:${branch}"
)
if ($tagLatest) {
  $tags += @("-t", "${registry}/webrtc-sip-gateway-stack:latest")
}

# During the operator-defined migration window, point legacy repository names at
# the same immutable stack image. Example: LEGACY_IMAGE_NAMES=k2-stack
if ($env:LEGACY_IMAGE_NAMES) {
  foreach ($legacyImage in $env:LEGACY_IMAGE_NAMES.Split(',', [System.StringSplitOptions]::RemoveEmptyEntries).Trim()) {
    $tags += @("-t", "${registry}/${legacyImage}:${branch}")
  }
}

Write-Host "docker-ci: registry=$registry branch=$branch platforms=$platforms tagLatest=$tagLatest"
docker buildx build --push `
  --platform $platforms `
  @tags `
  --build-arg VITE_GATEWAY_URL=$env:VITE_GATEWAY_URL `
  --build-arg VITE_CONFIG_AUTORECORD=$env:VITE_CONFIG_AUTORECORD `
  --build-arg VITE_BASE_PATH=$viteBasePath `
  -f deploy/Dockerfile.unified .
if ($LASTEXITCODE -ne 0) {
  throw "docker buildx build failed with exit code $LASTEXITCODE"
}

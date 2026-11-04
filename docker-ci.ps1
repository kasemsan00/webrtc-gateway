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
# Publish the immutable release tag and the current canonical release tag from
# the same multi-platform build manifest.
$tags = @(
  "-t", "${registry}/webrtc-sip-gateway-stack:${branch}",
  "-t", "${registry}/webrtc-sip-gateway-stack:latest"
)

# During the operator-defined migration window, point legacy repository names at
# the same immutable stack image. Example: LEGACY_IMAGE_NAMES=k2-stack
if ($env:LEGACY_IMAGE_NAMES) {
  foreach ($legacyImage in $env:LEGACY_IMAGE_NAMES.Split(',', [System.StringSplitOptions]::RemoveEmptyEntries).Trim()) {
    $tags += @("-t", "${registry}/${legacyImage}:${branch}")
  }
}

function Get-GitValue([string[]]$GitArgs) {
  try {
    $out = & git @GitArgs 2>$null
    if ($LASTEXITCODE -eq 0 -and $out) { return ($out | Select-Object -First 1).Trim() }
  } catch {}
  return "unknown"
}

$gitCommit = Get-GitValue @("rev-parse", "HEAD")
$gitBranch = Get-GitValue @("rev-parse", "--abbrev-ref", "HEAD")
$gitVersion = Get-GitValue @("describe", "--tags", "--always", "--dirty")
$buildTime = [DateTime]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ssZ")
$gitDirty = "unknown"
try {
  $porcelain = & git status --porcelain 2>$null
  if ($LASTEXITCODE -eq 0) { $gitDirty = if ($porcelain) { "true" } else { "false" } }
} catch {}
Write-Host "gateway build: commit=$gitCommit branch=$gitBranch version=$gitVersion dirty=$gitDirty time=$buildTime"

docker buildx build --push `
  --platform $platforms `
  @tags `
  --build-arg VITE_GATEWAY_URL=$env:VITE_GATEWAY_URL `
  --build-arg VITE_CONFIG_AUTORECORD=$env:VITE_CONFIG_AUTORECORD `
  --build-arg VITE_BASE_PATH=$viteBasePath `
  --build-arg GIT_COMMIT=$gitCommit `
  --build-arg GIT_BRANCH=$gitBranch `
  --build-arg GIT_VERSION=$gitVersion `
  --build-arg BUILD_TIME=$buildTime `
  --build-arg GIT_DIRTY=$gitDirty `
  -f deploy/Dockerfile.unified .

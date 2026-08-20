param(
  [string]$Root = (Resolve-Path (Join-Path $PSScriptRoot '..'))
)

$ErrorActionPreference = 'Stop'
$rootPath = (Resolve-Path -LiteralPath $Root).Path.TrimEnd('\', '/')

$allowedLegacyPaths = @{
  'apps/frontend/src/features/auth/password-auth.ts' = 'one-time session key migration'
  'apps/frontend/src/features/trunk/store/trunk-prefs-store.ts' = 'one-time local-storage key migration'
  'apps/frontend/src/lib/theme-store.ts' = 'one-time local-storage key migration'
  'apps/frontend/src/routes/__root.tsx' = 'pre-hydration read of the legacy theme key'
  'apps/frontend/src/features/auth/password-provider.test.tsx' = 'legacy session migration coverage'
  'apps/frontend/src/features/trunk/store/trunk-prefs-store.test.ts' = 'legacy preference migration coverage'
  'apps/frontend/src/lib/store-persist.test.ts' = 'generic migration coverage'
  'apps/gateway/internal/sip/product_identity_test.go' = 'asserts that the old product token is absent'
  'deploy/README.md' = 'documents the operator-controlled image compatibility window'
  'docker-ci.ps1' = 'supports publishing an explicit legacy image alias during migration'
  'scripts/check-product-identity.ps1' = 'defines the legacy identity scan pattern and allowlist'
}

$legacyPattern = '(?i)\b(?:k2-gateway|k2-frontend|k2-stack|k2-theme|k2_trunk_prefs|k2-admin-password|k2user|k2pass|k2_gateway|k2-bootstrap)\b|\bK2(?:\s+WebRTC)?\s+Gateway\b|\bTTRS-K2Gateway\b|\bwebrtc-gateway\b'
$excludedPrefixes = @(
  '.superpowers/',
  '.tmp-',
  'apps/frontend/dist/',
  'apps/gateway/logs/',
  'openspec/changes/archive/',
  'docs/superpowers/',
  'openspec/changes/rename-to-webrtc-sip-gateway/'
)

$violations = @()
git -C $rootPath ls-files --cached --others --exclude-standard | ForEach-Object {
  $relative = $_.Replace('\', '/')
  $excluded = $false
  foreach ($prefix in $excludedPrefixes) {
    if ($relative.StartsWith($prefix)) {
      $excluded = $true
      break
    }
  }
  if ($relative -match '(^|/)(node_modules|\.git)/|(^|/)\.env($|\.)' -or $excluded) {
    return
  }
  $path = Join-Path $rootPath $relative
  if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { return }
  if ((Get-Item -LiteralPath $path).Length -gt 2MB) { return }

  $matches = Select-String -LiteralPath $path -Pattern $legacyPattern -AllMatches
  if ($matches -and -not $allowedLegacyPaths.ContainsKey($relative)) {
    $matches | ForEach-Object {
      $violations += "${relative}:$($_.LineNumber): $($_.Line.Trim())"
    }
  }
}

if ($violations.Count -gt 0) {
  Write-Error ("Unexpected temporary product identity references:`n" + ($violations -join "`n"))
  exit 1
}

Write-Host 'Product identity scan passed.'

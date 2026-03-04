param(
    [Parameter(Mandatory = $false)]
    [string]$BuildFile
)

$ErrorActionPreference = 'Stop'

if (-not $BuildFile) {
    $ScriptDir = Split-Path -Parent $PSCommandPath
    $ProjectRoot = Split-Path -Parent $ScriptDir
    $AndroidDir = Join-Path $ProjectRoot 'android'
    $BuildFile = Join-Path $AndroidDir 'app\build.gradle'
}

$ScriptDir = Split-Path -Parent $PSCommandPath
$ProjectRoot = Split-Path -Parent $ScriptDir
$AndroidDir = Join-Path $ProjectRoot 'android'
$LibsDir = Join-Path $AndroidDir 'libs'
$SourceAar = Join-Path $ScriptDir 'webrtc-release.aar'
$TargetAar = Join-Path $LibsDir 'webrtc-release.aar'

if (-not (Test-Path -LiteralPath $AndroidDir)) {
    Write-Host "[patch-android] Android directory not found. Run 'expo prebuild' first." -ForegroundColor Red
    exit 1
}

if (-not (Test-Path -LiteralPath $SourceAar)) {
    Write-Host "[patch-android] Missing webrtc-release.aar next to this script." -ForegroundColor Red
    exit 1
}

if (-not (Test-Path -LiteralPath $LibsDir)) {
    New-Item -ItemType Directory -Path $LibsDir -Force | Out-Null
}

try {
    Copy-Item -LiteralPath $SourceAar -Destination $TargetAar -Force
} catch {
    Write-Host "[patch-android] Failed to copy webrtc-release.aar into android/libs." -ForegroundColor Red
    exit 1
}

if (-not (Test-Path -LiteralPath $BuildFile)) {
    throw "build.gradle not found: $BuildFile"
}

$lines = Get-Content -LiteralPath $BuildFile
$list = New-Object System.Collections.Generic.List[string]
$list.AddRange([string[]]$lines)

$implementationPattern = "implementation files\('../libs/webrtc-release\.aar'\)"
if (-not ($list | Where-Object { $_ -match $implementationPattern })) {
    for ($i = 0; $i -lt $list.Count; $i++) {
        if ($list[$i] -match 'implementation\("com\.facebook\.react:react-android"\)') {
            $list.Insert($i + 1, '    implementation files(''../libs/webrtc-release.aar'')')
            break
        }
    }
}

$excludePattern = 'exclude group:\s*"org\.jitsi"'
if (-not ($list | Where-Object { $_ -match $excludePattern })) {
    $block = [string[]](
        '',
        'configurations.all {',
        '    exclude group: "org.jitsi", module: "webrtc"',
        '}',
        ''
    )

    $inserted = $false
    for ($i = 0; $i -lt $list.Count; $i++) {
        if ($list[$i] -match '^\s*dependencies\s*\{') {
            $list.InsertRange($i, $block)
            $inserted = $true
            break
        }
    }

    if (-not $inserted) {
        $list.AddRange($block)
    }
}

Set-Content -LiteralPath $BuildFile -Value $list -Encoding utf8

Write-Host "[patch-android] WebRTC dependency patch applied successfully."

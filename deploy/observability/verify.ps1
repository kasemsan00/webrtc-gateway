$ErrorActionPreference = 'Stop'

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = (Resolve-Path (Join-Path $scriptRoot '..\..')).Path
$catalogPath = Join-Path $repoRoot 'docs\gateway\observability-catalog.json'
$catalog = Get-Content -Raw -LiteralPath $catalogPath | ConvertFrom-Json

function Assert-UniqueNames {
    param([string]$Label, [object[]]$Values)
    $duplicates = @($Values | Group-Object | Where-Object Count -gt 1 | ForEach-Object Name)
    if ($duplicates.Count -gt 0) {
        throw "$Label contains duplicates: $($duplicates -join ', ')"
    }
}

Assert-UniqueNames 'resourceFields' $catalog.resourceFields
Assert-UniqueNames 'recordFields' $catalog.recordFields
Assert-UniqueNames 'components' $catalog.components
Assert-UniqueNames 'eventNames' $catalog.eventNames
Assert-UniqueNames 'outcomes' $catalog.outcomes
Assert-UniqueNames 'reasons' $catalog.reasons
Assert-UniqueNames 'metric names' @($catalog.metrics | ForEach-Object name)
Assert-UniqueNames 'spanAttributes' $catalog.spanAttributes

# Negative policy fixtures prove the validator rejects duplicates rather than
# merely loading the current valid catalog.
$duplicateRejected = $false
try {
    Assert-UniqueNames 'deliberate duplicate fixture' @('fixture.name', 'fixture.name')
} catch {
    $duplicateRejected = $true
}
if (-not $duplicateRejected) {
    throw 'Duplicate-name policy did not reject its deliberate fixture'
}

$allowedMetricAttributes = @($catalog.metricAttributes.allowed)
$forbiddenFragments = @($catalog.metricAttributes.forbiddenNameFragments)
foreach ($metric in $catalog.metrics) {
    if ($metric.name -notmatch '^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)+$') {
        throw "Invalid metric name: $($metric.name)"
    }
    Assert-UniqueNames "attributes for $($metric.name)" @($metric.attributes)
    foreach ($attribute in $metric.attributes) {
        if ($attribute -notin $allowedMetricAttributes) {
            throw "Metric $($metric.name) uses non-allowlisted attribute $attribute"
        }
        foreach ($fragment in $forbiddenFragments) {
            $fragmentPattern = '(^|[._])' + [regex]::Escape($fragment) + '($|[._])'
            if ($attribute -match $fragmentPattern) {
                throw "Metric $($metric.name) uses unbounded/forbidden attribute $attribute"
            }
        }
    }
}

foreach ($forbiddenFixture in @('session.id', 'sip.call_id', 'phone.number', 'username',
        'client.ip', 'device.id', 'request.url', 'error.message', 'payload.body')) {
    $rejected = $false
    if ($forbiddenFixture -notin $allowedMetricAttributes) {
        $rejected = $true
    }
    foreach ($fragment in $forbiddenFragments) {
        $fragmentPattern = '(^|[._])' + [regex]::Escape($fragment) + '($|[._])'
        if ($forbiddenFixture -match $fragmentPattern) {
            $rejected = $true
        }
    }
    if (-not $rejected) {
        throw "Metric cardinality policy accepted deliberate forbidden fixture $forbiddenFixture"
    }
}

$configPaths = @(
    Join-Path $scriptRoot 'otel-collector-host-file.yaml'
    Join-Path $scriptRoot 'otel-collector-kubernetes.yaml'
)
foreach ($configPath in $configPaths) {
    $raw = Get-Content -Raw -LiteralPath $configPath
    $logBlocks = [regex]::Matches($raw, '(?ms)^    logs:\r?\n(?<body>.*?)(?=^    (metrics|traces):)')
    $logPipeline = @($logBlocks | ForEach-Object { $_.Groups['body'].Value } |
        Where-Object { $_ -match '(?m)^      receivers:' })
    if ($logPipeline.Count -ne 1) {
        throw "$(Split-Path -Leaf $configPath) must contain exactly one logs pipeline"
    }
    $logPipeline = $logPipeline[0]
    if ([regex]::Matches($logPipeline, 'filelog/gateway').Count -ne 1) {
        throw "$(Split-Path -Leaf $configPath) must select filelog/gateway exactly once in its logs pipeline"
    }
    if ($logPipeline -match 'otlp/gateway') {
        throw "$(Split-Path -Leaf $configPath) would duplicate gateway logs through OTLP and filelog"
    }
    if ($raw -match 'docker\.sock') {
        throw "$(Split-Path -Leaf $configPath) references the forbidden Docker socket"
    }
    foreach ($required in @('memory_limiter', 'resource/gateway', 'redaction/defense_in_depth', 'batch', 'file_storage')) {
        if ($raw -notmatch [regex]::Escape($required)) {
            throw "$(Split-Path -Leaf $configPath) is missing $required"
        }
    }
}

$gatewayFiles = Get-ChildItem -LiteralPath (Join-Path $repoRoot 'apps\gateway') -Filter '*.go' -Recurse |
    Where-Object { $_.Name -notlike '*_test.go' }
$printSites = @($gatewayFiles | Select-String -Pattern '\b(?:fmt|log)\.(?:Print|Printf|Println)\s*\(')
$documentedFamilies = @('internal\api', 'internal\sip', 'internal\session', 'internal\config',
    'internal\logstore', 'internal\dbbootstrap', 'internal\push', 'internal\translator',
    'internal\telemetry', 'internal\webrtc', 'internal\pkg\webrtc', 'main.go', 'internal\logger')
$unclassified = @($printSites | Where-Object {
    $relative = [System.IO.Path]::GetRelativePath((Join-Path $repoRoot 'apps\gateway'), $_.Path)
    -not ($documentedFamilies | Where-Object { $relative -like "$_*" })
})
if ($unclassified.Count -gt 0) {
    throw "Production logging source family is not classified: $($unclassified[0].Path):$($unclassified[0].LineNumber)"
}

$envExample = Get-Content -Raw -LiteralPath (Join-Path $scriptRoot '.env.example')
foreach ($required in @('OPENOBSERVE_OTLP_ENDPOINT', 'OPENOBSERVE_ORGANIZATION', 'OPENOBSERVE_LOG_STREAM',
        'OPENOBSERVE_METRIC_STREAM', 'OPENOBSERVE_TRACE_STREAM', 'OPENOBSERVE_AUTHORIZATION',
        'OPENOBSERVE_TLS_INSECURE', 'OTEL_EXPORTER_OTLP_ENDPOINT')) {
    if ($envExample -notmatch "(?m)^$required=") {
        throw "Collector .env.example is missing $required"
    }
}
if ($envExample -match '(?im)^OPENOBSERVE_AUTHORIZATION=(Basic|Bearer)\s+[A-Za-z0-9+/._=-]{12,}$') {
    throw 'Collector .env.example appears to contain a real authorization value'
}

$gatewayEnv = Get-Content -Raw -LiteralPath (Join-Path $repoRoot 'apps\gateway\.env.example')
foreach ($sensitiveDefault in @('DEBUG_TURN=false', 'DEBUG_SIP_MESSAGE=false', 'DEBUG_SIP_INVITE=false',
        'DB_LOG_FULL_SIP=false', 'PUSH_DEBUG_TTRS_TOKEN=false')) {
    if ($gatewayEnv -notmatch "(?m)^$([regex]::Escape($sensitiveDefault))$") {
        throw "Gateway .env.example must contain production-off default $sensitiveDefault"
    }
}

# Compare the implementation's OTEL environment contract with both operator
# documentation surfaces. This intentionally keys off quoted OTEL names in the
# config loader so newly supported settings cannot remain undocumented.
$configSource = Get-Content -Raw -LiteralPath (Join-Path $repoRoot 'apps\gateway\internal\config\config.go')
$configReference = Get-Content -Raw -LiteralPath (Join-Path $repoRoot 'docs\gateway\config-reference.md')
$implementedKeys = @([regex]::Matches($configSource, '"(OTEL_[A-Z0-9_]+)"') |
    ForEach-Object { $_.Groups[1].Value } | Sort-Object -Unique)
$exampleKeys = @([regex]::Matches($gatewayEnv, '(?m)^(OTEL_[A-Z0-9_]+)=') |
    ForEach-Object { $_.Groups[1].Value } | Sort-Object -Unique)
$documentedKeys = @([regex]::Matches($configReference, '`(OTEL_[A-Z0-9_]+)`') |
    ForEach-Object { $_.Groups[1].Value } | Sort-Object -Unique)
foreach ($key in $implementedKeys) {
    if ($key -notin $exampleKeys) {
        throw "Implemented observability setting $key is missing from apps/gateway/.env.example"
    }
    if ($key -notin $documentedKeys) {
        throw "Implemented observability setting $key is missing from docs/gateway/config-reference.md"
    }
}
foreach ($key in $exampleKeys) {
    if ($key -notin $implementedKeys) {
        throw "apps/gateway/.env.example contains unsupported observability setting $key"
    }
}

# Targeted repository check for newly introduced OpenObserve material. Real
# authorization must never appear in source; the reserved .invalid endpoint and
# explicit replacement marker are the only accepted examples.
$observabilityFiles = Get-ChildItem -LiteralPath $scriptRoot -File -Recurse
foreach ($file in $observabilityFiles) {
    $contents = Get-Content -Raw -LiteralPath $file.FullName
    if ($contents -match '(?im)^OPENOBSERVE_AUTHORIZATION=(Basic|Bearer)\s+[A-Za-z0-9+/._=-]{12,}$') {
        throw "Possible committed OpenObserve credential in $($file.FullName)"
    }
}

$docker = Get-Command docker -ErrorAction SilentlyContinue
if ($null -ne $docker) {
    foreach ($configPath in $configPaths) {
        $configName = Split-Path -Leaf $configPath
        & docker run --rm --env-file (Join-Path $scriptRoot '.env.example') `
            -v "${scriptRoot}:/conf:ro" otel/opentelemetry-collector-contrib:0.135.0 `
            validate --config "/conf/$configName"
        if ($LASTEXITCODE -ne 0) {
            throw "otelcol-contrib validation failed for $configName"
        }
    }
} else {
    Write-Warning 'Docker is unavailable; skipped otelcol-contrib validation.'
}

Write-Host "Observability verification passed: $($printSites.Count) production print sites classified; $($catalog.metrics.Count) metrics validated."

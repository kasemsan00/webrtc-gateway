param(
    [Parameter(Mandatory = $true)]
    [string]$Profile,
    [string]$Tag = "run",
    [switch]$DryRun,
    [string]$PerfSummaryUrl = "http://127.0.0.1:8080/api/perf-summary",
    [int]$CpuWindowSeconds = 300
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Get-ObjectValueOrDefault {
    param(
        [Parameter(Mandatory = $true)]
        [AllowNull()]
        [object]$Object,
        [Parameter(Mandatory = $true)]
        [string]$Key,
        [double]$Default = 0
    )

    if ($null -eq $Object) {
        return $Default
    }

    if ($Object -is [System.Collections.IDictionary]) {
        if ($Object.Contains($Key) -and $null -ne $Object[$Key]) {
            return [double]$Object[$Key]
        }
        return $Default
    }

    $property = $Object.PSObject.Properties[$Key]
    if ($null -ne $property -and $null -ne $property.Value) {
        return [double]$property.Value
    }

    return $Default
}

function Has-ObjectKey {
    param(
        [AllowNull()]
        [object]$Object,
        [Parameter(Mandatory = $true)]
        [string]$Key
    )

    if ($null -eq $Object) {
        return $false
    }

    if ($Object -is [System.Collections.IDictionary]) {
        return $Object.Contains($Key)
    }

    return $null -ne $Object.PSObject.Properties[$Key]
}

if (-not (Test-Path $Profile)) {
    throw "Profile not found: $Profile"
}

$profileJson = Get-Content -Raw -Path $Profile | ConvertFrom-Json

$resultsDir = Join-Path $PSScriptRoot "capacity\results"
New-Item -ItemType Directory -Force -Path $resultsDir | Out-Null

$startedAt = (Get-Date).ToUniversalTime()
$timestamp = $startedAt.ToString("yyyyMMdd-HHmmss")
$outputPath = Join-Path $resultsDir "$timestamp-$Tag.json"

$result = [ordered]@{
    tag       = $Tag
    dryRun    = [bool]$DryRun
    startedAt = $startedAt.ToString("o")
    profile   = $profileJson
    telemetry = [ordered]@{
        perfSummaryUrl = $PerfSummaryUrl
        source         = "runtime"
    }
    steps     = @()
}

$sessionStep = [int]$profileJson.ramp.sessionStep
$intervalSec = [int]$profileJson.ramp.intervalSeconds
$counterBaseline = $null

for ($i = 1; $i -le 4; $i++) {
    $targetSessions = $sessionStep * $i
    $perf = $null
    $telemetryFetchOk = $true
    try {
        $perf = Invoke-RestMethod -Method Get -Uri $PerfSummaryUrl -TimeoutSec 5
    } catch {
        $telemetryFetchOk = $false
        $perf = [ordered]@{
            counters    = [ordered]@{}
            timersMsP95 = [ordered]@{}
            runtime     = [ordered]@{ goroutines = 0; heapAllocBytes = 0 }
        }
    }

    $hasRuntime = Has-ObjectKey -Object $perf -Key "runtime"
    $hasCounters = Has-ObjectKey -Object $perf -Key "counters"
    $hasTimers = Has-ObjectKey -Object $perf -Key "timersMsP95"
    $hasOutgoingTimer = Has-ObjectKey -Object $perf.timersMsP95 -Key "sip_outgoing_setup_ms"
    $hasForwardTimer = Has-ObjectKey -Object $perf.timersMsP95 -Key "rtp_forward_loop_ms"
    $hasSetupTimeoutCounter = Has-ObjectKey -Object $perf.counters -Key "sip_setup_timeout_total"
    $hasWsBackpressureCounter = Has-ObjectKey -Object $perf.counters -Key "ws_send_queue_full_total"
    $hasLossSpikeCounter = Has-ObjectKey -Object $perf.counters -Key "rtp_loss_spike_total"
    $telemetryComplete = $telemetryFetchOk -and $hasRuntime -and $hasCounters -and $hasTimers -and
        $hasOutgoingTimer -and $hasForwardTimer -and $hasSetupTimeoutCounter -and $hasWsBackpressureCounter -and $hasLossSpikeCounter

    $setupTimeoutTotal = Get-ObjectValueOrDefault -Object $perf.counters -Key "sip_setup_timeout_total"
    $wsQueueFullTotal = Get-ObjectValueOrDefault -Object $perf.counters -Key "ws_send_queue_full_total"
    $lossSpikeTotal = Get-ObjectValueOrDefault -Object $perf.counters -Key "rtp_loss_spike_total"
    if ($telemetryComplete -and $null -eq $counterBaseline) {
        $counterBaseline = [ordered]@{
            setupTimeout = $setupTimeoutTotal
            wsQueueFull  = $wsQueueFullTotal
            lossSpike    = $lossSpikeTotal
        }
    }
    $setupTimeoutEvents = if ($null -ne $counterBaseline) { [math]::Max(0.0, $setupTimeoutTotal - $counterBaseline.setupTimeout) } else { 0.0 }
    $wsQueueFullEvents = if ($null -ne $counterBaseline) { [math]::Max(0.0, $wsQueueFullTotal - $counterBaseline.wsQueueFull) } else { 0.0 }
    $lossSpikeEvents = if ($null -ne $counterBaseline) { [math]::Max(0.0, $lossSpikeTotal - $counterBaseline.lossSpike) } else { 0.0 }

    if ($telemetryComplete -and $targetSessions -gt 0) {
        $setupSuccessRate = [math]::Round([math]::Max(0.0, 1.0 - ($setupTimeoutEvents / [double]$targetSessions)), 4)
    } else {
        $setupSuccessRate = -1.0
    }
    $setupLatencyP95Ms = Get-ObjectValueOrDefault -Object $perf.timersMsP95 -Key "sip_outgoing_setup_ms"
    $rtpForwardP95Ms = Get-ObjectValueOrDefault -Object $perf.timersMsP95 -Key "rtp_forward_loop_ms"
    $goroutines = Get-ObjectValueOrDefault -Object $perf.runtime -Key "goroutines"
    $heapAllocBytes = Get-ObjectValueOrDefault -Object $perf.runtime -Key "heapAllocBytes"
    $cpuSampleSeconds = [math]::Max(1, $CpuWindowSeconds)
    $cpuCounter = Get-Counter -Counter '\Processor(_Total)\% Processor Time' -SampleInterval 1 -MaxSamples $cpuSampleSeconds -ErrorAction SilentlyContinue
    $cpuPercentAvg5m = if ($null -ne $cpuCounter -and $cpuCounter.CounterSamples.Count -gt 0) {
        [math]::Round((($cpuCounter.CounterSamples | Measure-Object -Property CookedValue -Average).Average), 2)
    } else {
        -1.0
    }
    $memoryPercent = [math]::Round((($heapAllocBytes / 1GB) / 16.0) * 100.0, 2)
    $qualityAlarmWindowSec = if ($lossSpikeEvents -gt 0) { 30.0 } else { 0.0 }
    $hasFiveMinuteCpuWindow = $cpuSampleSeconds -ge 300

    $pass = $telemetryComplete -and
        ($setupSuccessRate -ge 0.99) -and
        ($setupLatencyP95Ms -le 2500) -and
        ($rtpForwardP95Ms -le 20) -and
        $hasFiveMinuteCpuWindow -and ($cpuPercentAvg5m -ge 0) -and ($cpuPercentAvg5m -le 75) -and
        ($memoryPercent -le 80) -and
        ($wsQueueFullEvents -le 1) -and
        ($qualityAlarmWindowSec -le 15)

    $status = if ($DryRun) {
        "simulated"
    } elseif (-not $telemetryFetchOk) {
        "telemetry-unavailable"
    } elseif (-not $hasFiveMinuteCpuWindow) {
        "telemetry-incomplete"
    } elseif (-not $telemetryComplete) {
        "telemetry-incomplete"
    } else {
        "executed"
    }

    $result.steps += [ordered]@{
        stepIndex             = $i
        targetSessions        = $targetSessions
        intervalSeconds       = $intervalSec
        status                = $status
        telemetryComplete     = $telemetryComplete
        cpuWindowSeconds      = $cpuSampleSeconds
        setupSuccessRate      = $setupSuccessRate
        setupLatencyP95Ms     = $setupLatencyP95Ms
        rtpForwardLoopP95Ms   = $rtpForwardP95Ms
        cpuPercentAvg5m       = $cpuPercentAvg5m
        memoryPercent         = $memoryPercent
        goroutineCount        = $goroutines
        wsQueueFullEvents     = $wsQueueFullEvents
        qualityAlarmWindowSec = $qualityAlarmWindowSec
        bottleneck            = $(if (-not $telemetryFetchOk) { "telemetry-unavailable" } elseif (-not $hasFiveMinuteCpuWindow) { "telemetry-incomplete" } elseif (-not $telemetryComplete) { "telemetry-incomplete" } elseif ($pass) { "none-observed" } else { "quality-or-resource-gate" })
        pass                  = $pass
    }
}

$finishedAt = (Get-Date).ToUniversalTime()
$result.finishedAt = $finishedAt.ToString("o")

$result | ConvertTo-Json -Depth 8 | Set-Content -Path $outputPath -Encoding UTF8
Write-Host "Capacity runner result written: $outputPath"

# Gateway Call-Flow, RTP Performance, and Capacity Design

Date: 2026-03-15  
Status: Approved for planning  
Scope: `apps/gateway` only

## 1) Problem Statement

The gateway must improve runtime stability for outgoing/incoming call handling, reduce RTP-related quality issues (audio jitter/drop, video freeze/keyframe delay), and produce an evidence-based per-instance concurrency limit.

Constraints:

- Keep wire compatibility (WebSocket and SIP contract behavior unchanged).
- Preserve media invariants (Opus passthrough and H.264 behavior, including SPS/PPS handling).
- Prioritize stable quality first, then maximize concurrency.
- Target sizing baseline: single instance `8 vCPU / 16 GB RAM`.

## 2) Proposed Approach

Use a phased, telemetry-gated workflow:

1. RTP performance tuning in media path.
2. Outgoing/incoming call-flow bug hunt and hardening.
3. Capacity estimation with stepped load and acceptance gates.

Each phase uses a strict baseline -> change -> re-measure loop. A phase only advances when quality and stability gates are met.

## 3) Architecture and Boundaries

Work remains inside gateway internals:

- `internal/session/*` (RTP forward loop, track handlers, session state)
- `internal/sip/*` (incoming/outgoing call signaling paths)
- `internal/api/server.go` (WS lifecycle and delivery pressure points)
- `internal/logstore/*` and runtime metrics surfaces (for observability)

No protocol schema changes are planned. Improvements are internal behavior, safety checks, and instrumentation.

## 4) Components and Data Flow

### 4.1 perf-observer layer

Purpose: capture consistent before/after evidence for performance and stability.

Owner surfaces and contracts:

- `internal/observability/metrics.go` (new package): typed metric registration/update helpers.
- `internal/api/handlers.go`: read-only JSON summary endpoint `/api/perf-summary`.
- `internal/session/*`, `internal/sip/*`, `internal/api/server.go`: emit structured counters/timers.

Inputs:

- RTP forwarding loop timing and packet handling paths
- Session creation/lookup/deletion hotspots
- SIP incoming/outgoing handling latency
- WS client send pressure and drop/error signals
- Goroutine and memory trend snapshots

Outputs:

- `PerfSummary` JSON payload with 1-minute and 5-minute windows.
- Log snapshots at fixed interval (60s) for environments without dashboard scraping.

Dependencies:

- `runtime` stats (`NumGoroutine`, mem stats)
- existing logger package

### 4.2 call-flow-checker harness

Purpose: deterministic outgoing/incoming correctness verification.

Owner surfaces and contracts:

- `internal/testsupport/callflow` (new): reusable scenario runner helpers.
- `internal/api/*_test.go` and `internal/sip/*_test.go`: scenario entry tests.

Inputs:

- Browser -> gateway -> SIP outgoing call
- SIP -> gateway -> browser incoming call
- Mid-call stability checks (state transitions, media continuity)

Outputs:

- machine-readable test report (`pass/fail`, timing, error category)
- reproducible seed/config attached to each run

### 4.3 capacity-runner profile

Purpose: produce reproducible concurrency boundary per deployment shape.

Owner surfaces and contracts:

- `apps/gateway/scripts/capacity-runner.ps1` (or equivalent existing test runner location): stepped load orchestration.
- `apps/gateway/scripts/load-profile.json`: fixed profile for codec mix and durations.

Use stepped concurrency runs on 8 vCPU/16 GB:

- Increment session counts in fixed steps.
- Keep traffic profile stable between runs.
- Compare against baseline metrics to identify first failure threshold.

Input profile (fixed for comparison):

- codec mix: 70% audio-only (Opus), 30% audio+video (H.264)
- call duration: 5 minutes/session
- ramp: +25 sessions every 2 minutes
- network condition: baseline LAN, and one impaired profile (1% packet loss, 30ms jitter)

Outputs:

- per-step result file (JSON): resource use, quality counters, pass/fail
- final recommendation: max safe sessions and dominant bottleneck category

## 5) Reliability and Bug Strategy

Priority targets:

1. RTP lock contention and packet-path stalls (especially H.264/STAP-A related pressure).
2. Goroutine lifecycle leaks in track/RTCP loops and cleanup boundaries.
3. Session lookup scalability under signaling load.
4. WS event delivery pressure and state visibility under load.

Guardrails:

- No broad silent fallbacks.
- Explicit counters/log signals for degraded states.
- Race-focused validation on touched packages (`go test -race` where practical).

Failure-mode expectations:

- SIP timeout: fail the affected call setup within configured timeout, emit `sip_setup_timeout_total`, keep process healthy.
- RTP packet-loss spike: emit `rtp_loss_spike_total`, maintain session unless loss persists > 10s.
- WS backpressure exhaustion: emit `ws_send_queue_full_total`, mark client degraded, do not crash session manager.
- Partial cleanup failure: emit `session_cleanup_error_total`, retry cleanup in bounded background attempts.

## 6) Capacity Methodology

Capacity is derived from test evidence, not fixed assumptions:

- Execute stepped load (e.g., 50 -> 100 -> 200 -> ... sessions).
- At each step evaluate quality and resource gates:
  - call setup success rate >= 99%
  - p95 outgoing/incoming call setup latency <= 2.5s
  - p95 RTP forwarding loop processing <= 20ms
  - CPU <= 75% sustained (5-minute average), memory <= 80% of 16 GB
  - goroutines <= baseline + (8 * active_sessions) and no upward leak trend after steady state
  - ws queue full events <= 1 per 1,000 session-minutes
  - no continuous quality alarm window > 15s (audio drop or video freeze counters)

The first failing step marks the boundary. The previous passing step becomes the recommended per-instance production ceiling (with safety margin).

## 7) Testing Strategy

For each phase:

1. Capture baseline telemetry and behavior.
2. Apply a focused batch of changes.
3. Re-run the same scenarios.
4. Compare p95 latency, quality symptoms, resource usage, and error counters.

Execution path:

- Local/manual: run from `apps/gateway` with documented script + env profile.
- CI/nightly: run reduced-profile smoke capacity test and full profile on scheduled pipeline.

Regression focus:

- Outgoing/incoming call setup and teardown correctness
- Media-path stability during sustained load
- State transition correctness and cleanup behavior

## 8) Acceptance Criteria

- Outgoing and incoming call validation scenarios pass.
- No new race findings in touched packages.
- RTP quality symptoms improved versus baseline:
  - >= 30% reduction in freeze/drop event counters per session-minute, or
  - maintain equal quality while supporting >= 20% more concurrent sessions.
- Documented safe concurrency result for 8 vCPU/16 GB with observed bottleneck attribution.

## 9) Deliverables

- A prioritized hotspot/bug list with severity and action.
- Performance comparison report (baseline vs post-change).
- Capacity recommendation per instance with confidence notes.
- Follow-up tuning backlog ordered by impact and risk.

## 10) Out of Scope

- Breaking WebSocket/SIP contract changes.
- Cross-service architecture redesign.
- Unrelated refactors outside call/media/capacity objectives.

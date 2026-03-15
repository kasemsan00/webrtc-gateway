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

Add lightweight instrumentation around:

- RTP forwarding loop timing and packet handling paths
- Session creation/lookup/deletion hotspots
- SIP incoming/outgoing handling latency
- WS client send pressure and drop/error signals
- Goroutine and memory trend snapshots

### 4.2 call-flow-checker harness

Define deterministic validation flows for:

- Browser -> gateway -> SIP outgoing call
- SIP -> gateway -> browser incoming call
- Mid-call stability checks (state transitions, media continuity)

### 4.3 capacity-runner profile

Use stepped concurrency runs on 8 vCPU/16 GB:

- Increment session counts in fixed steps.
- Keep traffic profile stable between runs.
- Compare against baseline metrics to identify first failure threshold.

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

## 6) Capacity Methodology

Capacity is derived from test evidence, not fixed assumptions:

- Execute stepped load (e.g., 50 -> 100 -> 200 -> ... sessions).
- At each step evaluate quality and resource gates:
  - No obvious audio/video instability trend
  - Bounded goroutine growth
  - Acceptable CPU and memory headroom
  - No increasing error/drop pattern

The first failing step marks the boundary. The previous passing step becomes the recommended per-instance production ceiling (with safety margin).

## 7) Testing Strategy

For each phase:

1. Capture baseline telemetry and behavior.
2. Apply a focused batch of changes.
3. Re-run the same scenarios.
4. Compare p95 latency, quality symptoms, resource usage, and error counters.

Regression focus:

- Outgoing/incoming call setup and teardown correctness
- Media-path stability during sustained load
- State transition correctness and cleanup behavior

## 8) Acceptance Criteria

- Outgoing and incoming call validation scenarios pass.
- No new race findings in touched packages.
- RTP quality symptoms are reduced relative to baseline.
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

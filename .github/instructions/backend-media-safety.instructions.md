---
description: "Use when modifying Go code in gateway-sip, especially media forwarding, SIP signaling, WebRTC session handling, or concurrency logic. Covers media stability, race-free concurrency, SIP/WS contract alignment, and production resilience requirements."
applyTo: "apps/gateway-sip/**"
---

# Backend Media Safety (gateway-sip)

## Mission-Critical Stability Rules

**This service is in the media path.** A minor bug can cause dropped calls, one-way audio, or black video.

1. **Stability first** — no panics in hot paths (RTP/RTCP/SIP loops), log and continue.
2. **Concurrency discipline is mandatory**:
   - Protect shared state with `sync.RWMutex`; **never hold locks during network I/O**.
   - Run data-race detector in tests: `go test -race ./...`
3. **Do not break media invariants**:
   - Audio: **Opus passthrough** end-to-end (no transcoding).
   - Video: **H.264 only** (profile-level-id + sprop-parameter-sets handling required).
   - SPS/PPS caching and keyframe injection logic must stay intact.
4. **Protocol compatibility is product behavior** — WebSocket and SIP interop changes are breaking changes affecting mobile/web clients.

## Media Path Specifics

### Audio

- Opus-only passthrough, browser ↔ gateway ↔ SIP peer.
- DTMF uses RFC2833 (`telephone-event`, usually PT 101).
- No audio transcoding or mixing.

### Video

- H.264 only; preserve SPS/PPS caching in `internal/session/h264_paramsets.go`.
- Keyframe injection strategy in `internal/session/keyframe.go` — do not remove reinjection logic.
- SDP offer generation in `internal/sip/sdp.go` — always include profile-level-id + sprop handling.
- Keyframe recovery: Use FIR sequence numbers (not redundant PLIs) to avoid jitter buffer corruption.
- Watchdog intervals: PLI=5s, FIR=10s (prevents flooding on demand-only keyframes).

## Concurrency and Hot Paths

- **RTP/RTCP forwarding loops** (`internal/session/rtp_forward.go`) must avoid panics; log errors and continue.
- **Session state mutations** (`internal/session/session.go`) require `sync.RWMutex`; never hold locks during WebRTC update/negotiate.
- **SIP dialogs** (`internal/sip/dialog.go`) are append-only for call history safety.
- **No data races**: Pre-test all changes with `go test -race ./...`

## SIP/WebSocket Contract Alignment

### WebSocket Endpoint: `/ws`

Payload format: JSON

#### Client → Server message types

- `offer` — requires `sdp` (`sessionId` optional for existing session)
- `call` — requires `sessionId`, `destination` (public or trunk mode)
- `hangup`, `accept`, `reject`, `dtmf`, `send_message`, `resume`, `trunk_resolve`, `ping`

#### Server → Client message types

- `answer`, `state`, `incoming`
- `message`, `messageSent`, `dtmf`
- `resumed`, `resume_failed`, `resume_redirect`
- `trunk_resolved`, `trunk_redirect`, `trunk_not_found`, `trunk_not_ready` (now returns both `trunkId` and `trunkPublicId`)
- `pong`, `error`

**When adding/changing message types:**

1. Update `internal/api/server.go` switch + payload struct.
2. Update frontend client in `webrtc-gateway-react` (`src/features/gateway/store/gateway-store.ts`).
3. Update this documentation + mobile clients.

## Session Resume & Resilience

- Session resume after transport changes is **first-class behavior** (`resume` flow).
- Session directory (`internal/logstore/`) and gateway registry enable cross-instance redirect.
- Incoming call acceptance uses first-accept-wins claim semantics.
- Never lose session state during resume; validate SDP compatibility before renegotiate.

## Database & Persistence

### LogStore (optional Postgres)

- Append-only event/payload insertion (no updates to call history).
- Async queues for events/stats — do not block RTP/RTCP hot paths.
- Partition maintenance: Future partitions auto-created on startup.
- Schema in `init.sql` (sessions, events, payloads, stats, dialogs, trunks, session directory, gateway instances).

### When DB is disabled

- `noopStore` keeps runtime behavior identical without persistence.
- Operational checks remain the same; just no logs in database.

## Configuration Reference

### SIP/API/RTP

- `SIP_LOCAL_IP` (default `0.0.0.0`) — **must be explicit IPv4 on Windows/Mac to prevent IPv6 Via headers breaking NAT**.
- `SIP_PUBLIC_IP` (optional, required behind NAT)
- `API_PORT` (default `8080`), `API_ENABLE_WS` (default `true`), `API_ENABLE_REST` (default `true`)
- `RTP_PORT_MIN/MAX` (default 10500–10600); increase `RTP_BUFFER_SIZE` if video blackscreen on Android.

### Debug flags (use sparingly in production)

- `DEBUG_WEBSOCKET`, `DEBUG_TURN`, `DEBUG_SIP_MESSAGE`, `DEBUG_SIP_INVITE`
- Media toggle: `SIP_AUDIO_USE_AVPF`, `SIP_VIDEO_USE_AVPF`, `SIP_VIDEO_PRESERVE_STAPA`
- Keyframe watchdog: `SIP_VIDEO_KEYFRAME_WATCHDOG` (default `true`)

Full reference in `apps/gateway-sip/.env.example`.

## Build, Test, Run

```bash
# Build
go build -o k2-gateway .

# Run
./k2-gateway

# Test (with race detection)
go test -race ./...

# Coverage
go test -covermode=atomic -coverprofile=coverage.out ./...
go tool cover -func=coverage.out

# Docker
docker build -t gateway-sip:local .
```

## Key Files to Review Before Changes

- `internal/session/session.go` — WebRTC session lifecycle
- `internal/session/rtp_forward.go` — media forwarding (must not panic)
- `internal/sip/sdp.go` — SDP generation/parsing (H.264 profile handling)
- `internal/api/server.go` — WebSocket message routing
- `go.mod` — locked dependency versions (pion/\* is critical)

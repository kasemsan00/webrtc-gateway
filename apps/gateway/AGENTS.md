# K2 Gateway - AI Agent Development Guide

> **Role:** Senior Go Engineer / Systems Architect
> **Objective:** Keep a production-grade WebRTC <-> SIP bridge stable under real-time load.
> **Hard constraint:** No panics, no races, no media regressions.

---

## 1. Mission-Critical Rules

1. **Stability first.** This service is in the media path. A minor bug can become dropped calls, one-way audio, or black video.
2. **Concurrency discipline is mandatory.** Protect shared state with `sync.RWMutex`; do not hold locks while doing network I/O.
3. **Do not break media invariants.**
   - Audio is **Opus passthrough** end-to-end (no transcoding).
   - Video is **H.264 only**.
   - SPS/PPS caching and keyframe injection logic must stay intact.
4. **Never panic in hot paths.** RTP/RTCP/SIP loops should log and continue when possible.
5. **Treat protocol compatibility as product behavior.** WebSocket and SIP interop changes are breaking changes.

---

## 2. Current Project Snapshot

K2 Gateway bridges WebRTC clients (browser/mobile) to SIP/RTP endpoints (Asterisk, Kamailio).

- **Language:** Go 1.26.2 (`go.mod`)
- **Core libs:** `pion/webrtc/v4`, `emiago/sipgo`, `gorilla/websocket`, `gorilla/mux`, `pion/rtp`, `pion/rtcp`, `pion/sdp/v3`, `pgx/v5`, `golang-jwt/jwt/v5`
- **Runtime mode:** API mode (HTTP + WebSocket)
- **Data plane:** SRTP (WebRTC side) <-> RTP/RTCP (SIP side)
- **Control plane:** JSON over WebSocket/REST <-> SIP signaling
- **Persistence:** Optional Postgres-backed LogStore (no-op implementation when `DB_ENABLE=false`)

---

## 3. Runtime Architecture (As Implemented)

1. `main.go` loads env config, initializes logger, starts LogStore, and boots API mode.
2. `internal/auth/*` loads JWKS and verifies JWT (`iss`, `aud`, `exp/nbf`, RSA signature).
3. `internal/api/server.go` handles `/ws`, session offers/answers, call control, resume, trunk resolve, SIP messaging, DTMF, and HTTP/WS auth enforcement.
4. `internal/session/*` owns per-call state and WebRTC PeerConnection lifecycle.
5. `internal/sip/*` handles SIP server/client behavior, INVITE/ACK/BYE/MESSAGE/DTMF, SDP generation/parsing, public registry, and trunk manager.
6. `internal/logstore/*` persists events/payloads/stats/dialog/session snapshots and instance/session directories.

---

## 4. Repository Map (Current)

> Root-level `docs/` contains client integration guides and call flows: see `docs/dual-flow.md`, `docs/call-resume.md`, `docs/web.md`, `docs/react-native.md`, `docs/ios.md`, `docs/android.md`.

```text
k2-gateway/
|- main.go
|- internal/
|  |- api/
|  |  |- server.go
|  |  |- auth_http.go
|  |  `- handlers.go
|  |- auth/
|  |  `- verifier.go
|  |- config/config.go
|  |- logger/logger.go
|  |- logstore/
|  |  |- logstore.go
|  |  |- queue.go
|  |  |- partition.go
|  |  `- models.go
|  |- session/
|  |  |- manager.go
|  |  |- session.go
|  |  |- session_state.go
|  |  |- session_media.go
|  |  |- rtp_forward.go
|  |  |- rtp_cache.go
|  |  |- keyframe.go
|  |  |- h264_paramsets.go
|  |  |- sdp_h264.go
|  |  |- media_engine.go
|  |  |- media_endpoints.go
|  |  `- renegotiate.go
|  |- sip/
|  |  |- server.go
|  |  |- handlers.go
|  |  |- call.go
|  |  |- dialog.go
|  |  |- registration.go
|  |  |- public_registry.go
|  |  |- trunk_manager.go
|  |  |- trunk_public_id.go
|  |  |- sdp.go
|  |  |- rtp.go
|  |  |- dtmf.go
|  |  |- message.go
|  |  |- ice.go
|  |  `- logging.go
|  |- webrtc/webrtc.go
|  `- pkg/webrtc/utils.go
|- init.sql
|- docker-compose.yml
`- .env.example
```

---

## 5. WebSocket Contract (Source of Truth)

Endpoint: `/ws`  
Payload format: JSON

Auth behavior:

- When `AUTH_ENABLE=true`, `/ws` requires `access_token` query parameter (`/ws?access_token=<jwt>`).
- Token is validated against configured JWKS/issuer/audience before WebSocket upgrade.

### Client -> Server message types

- `offer` -> requires `sdp` (`sessionId` optional for existing session)
- `call` -> requires `sessionId`, `destination` (`from` optional)
  - Public mode: include `sipDomain`, `sipUsername`, `sipPassword`, optional `sipPort`
  - Trunk mode: include `trunkId` or `trunkPublicId`
- `hangup` -> requires `sessionId`
- `accept` -> requires `sessionId`
- `reject` -> requires `sessionId` (`reason` optional, defaults to `busy`)
- `dtmf` -> requires `sessionId`, `digits`
- `send_message` -> requires `body`; use in-dialog if session exists, otherwise requires `destination`
- `resume` -> requires `sessionId`, optional `sdp`
- `trunk_resolve` -> requires `sipDomain`, `sipUsername`, `sipPassword`, optional `sipPort` (resolve-only; no auto-create)
- `ping` -> keepalive

### Server -> Client message types

- `answer`, `state`, `incoming`
- `message`, `messageSent`, `dtmf`
- `resumed`, `resume_failed`, `resume_redirect`
- `trunk_resolved`, `trunk_redirect`, `trunk_not_found`, `trunk_not_ready`
  - `trunk_resolved` now returns both `trunkId` and `trunkPublicId`
- `pong`, `error`

If you add/change a message type, update all of:

1. `internal/api/server.go` switch + payload struct
2. Frontend client handlers/senders in `frontend` (`src/features/gateway/store/gateway-store.ts`)
3. this document

---

## 6. Critical Media Behaviors (Do Not Regress)

### 6.1 Audio path

- Opus-only passthrough, browser <-> gateway <-> SIP peer.
- No audio transcoding.
- DTMF uses RFC2833 (`telephone-event`, usually PT 101).

### 6.2 Video path

- H.264 only.
- Preserve SPS/PPS caching and reinjection strategy (`internal/session/h264_paramsets.go`, `internal/session/keyframe.go`, `internal/sip/sdp.go`).
- Do not remove profile-level-id + sprop handling in SDP offer generation.

### 6.3 Recovery / resilience behavior

- Session resume after transport changes (`resume` flow) is first-class.
- Session directory and gateway registry are used for cross-instance redirect.
- Incoming call acceptance uses first-accept-wins claim semantics.

---

## 7. Configuration Reference (From `internal/config/config.go`)

### Core SIP/API/RTP

- `SIP_LOCAL_IP` (default `0.0.0.0`)
- `SIP_PUBLIC_IP` (optional, important behind NAT)
- `SIP_PORT` (default `5060`)
- `SIP_LOCAL_PORT` (default `5060`)
- `API_PORT` (default `8080`)
- `API_ENABLE_WS` (default `true`)
- `API_ENABLE_REST` (default `true`)
- `API_CORS_ORIGINS` (default `*`)
- `RTP_PORT_MIN` (default `10500`)
- `RTP_PORT_MAX` (default `10600`)
- `RTP_BUFFER_SIZE` (default `16384`)

### JWT auth (Keycloak/JWKS)

- `AUTH_ENABLE` (default `false`)
- `AUTH_JWKS_URL` (required when auth enabled)
- `AUTH_JWT_ISSUER` (required when auth enabled)
- `AUTH_JWT_AUDIENCE` (required when auth enabled)
- `AUTH_JWKS_TIMEOUT_MS` (default `5000`)

When `AUTH_ENABLE=true`:

- Startup is fail-fast if required auth env is missing.
- Startup is fail-fast if initial JWKS prefetch fails.
- `/api/*` requires `Authorization: Bearer <jwt>`.
- `/ws` requires `?access_token=<jwt>`.

### Debug and media behavior toggles

- `DEBUG_WEBSOCKET` (default `false`)
- `DEBUG_TURN` (default `false`)
- `DEBUG_SIP_MESSAGE` (default `false`)
- `DEBUG_SIP_INVITE` (default `false`)
- `SWITCH_PLI_DELAY_MS` (default `1000`)
- `SIP_AUDIO_USE_AVPF` (default `false`)
- `SIP_VIDEO_USE_AVPF` (default `false`)
- `SIP_VIDEO_FEEDBACK_TRANSPORT` (default `auto`; `auto|rtp|rtcp|dual`)
- `SIP_VIDEO_PRESERVE_STAPA` (default `false`)
- `SIP_VIDEO_KEYFRAME_WATCHDOG` (default `true`)
- `SIP_VIDEO_KEYFRAME_WATCHDOG_INTERVAL_MS` (default `1000`)
- `SIP_VIDEO_KEYFRAME_STALE_MS` (default `2500`)
- `SIP_VIDEO_KEYFRAME_FIR_STALE_MS` (default `6000`)

### TURN

- `TURN_SERVER`, `TURN_USERNAME`, `TURN_PASSWORD`

### Database / LogStore

- `DB_ENABLE` (default `false`)
- `DB_DSN`
- `DB_STATS_INTERVAL_MS` (default `5000`)
- `DB_LOG_FULL_SIP` (default `false`)
- `DB_BATCH_SIZE` (default `100`)
- `DB_BATCH_INTERVAL_MS` (default `1000`)
- `DB_PARTITION_LOOKAHEAD_DAYS` (default `7`)
- `DB_RETENTION_PAYLOADS_DAYS` (default `730`)
- `DB_RETENTION_EVENTS_DAYS` (default `730`)
- `DB_RETENTION_STATS_DAYS` (default `730`)
- `DB_RETENTION_SESSIONS_DAYS` (default `730`)

### SIP public mode

- `SIP_PUBLIC_REGISTER_EXPIRES_SECONDS` (default `3600`)
- `SIP_PUBLIC_REGISTER_TIMEOUT_SECONDS` (default `10`)
- `SIP_PUBLIC_IDLE_TTL_SECONDS` (default `600`)
- `SIP_PUBLIC_CLEANUP_INTERVAL_SECONDS` (default `30`)
- `SIP_PUBLIC_MAX_ACCOUNTS` (default `1000`)

### Trunk / multi-instance

- `SIP_TRUNK_ENABLE` (default follows `DB_ENABLE`)
- `SIP_TRUNK_LEASE_TTL_SECONDS` (default `60`)
- `SIP_TRUNK_LEASE_RENEW_INTERVAL_SECONDS` (default `20`)
- `SIP_TRUNK_REGISTER_TIMEOUT_SECONDS` (default `10`)
- `GATEWAY_INSTANCE_ID` (default hostname/random)
- `GATEWAY_PUBLIC_WS_URL` (for redirects)
- `SESSION_DIRECTORY_TTL_SECONDS` (default `7200`)
- `SESSION_DIRECTORY_CLEANUP_INTERVAL_SECONDS` (default `300`)

### Compatibility override (use carefully)

- `SIP_FORCE_AVP` (read in `internal/sip/sdp.go`) can force `RTP/AVP` even when AVPF flags are enabled.

---

## 8. SIP Public and Trunk Details

### Public account key behavior (`internal/sip/public_registry.go`)

- Hostname domain keys: `username@domain` (port omitted)
- IP-literal keys: `username@ip:port` (IPv6 bracketed)

### Destination resolution

- Hostname + explicit port -> dial that port
- Hostname + port 0 -> SRV lookup (`_sip._<transport>`) then fallback to `:5060`
- IP literal -> direct `ip:port` (or `:5060` when zero)

### Trunk manager

- Uses DB lease ownership (`lease_owner`, `lease_until`) to enforce single-active registration per trunk.
- Supports force unregister API via REST endpoints.
- Trunks are soft-deleted by update flow (`enabled=false`); rows are not hard-deleted from `sip_trunks`.
- Provides dual ID support:
  - internal numeric `trunkId` (DB primary key),
  - `trunkPublicId` from DB column `sip_trunks.public_id` (`UUID UNIQUE`) for external/API use.
- REST `/api/trunks` supports filters `trunkId` and `trunkPublicId`, and trunk responses include `publicId`.
- Trunk REST now supports update flow:
  - `PUT /api/trunk/{id}` for partial updates (`name`, `domain`, `port`, `username`, `password`, `transport`, `enabled`, `isDefault`, `updatedBy`).
  - Active-call safety policy: when active calls exist on trunk, reject with `409` for:
    - disabling trunk (`enabled=false`)
    - editing critical fields (`domain`, `port`, `username`, `transport`)
  - On successful update to `enabled=false`, gateway performs best-effort unregister and lease release.
  - `public_id` is immutable after creation.
  - Trunk responses expose both `public_id` and `publicId` during migration compatibility window.

---

## 9. Logging and Persistence

- LogStore uses async queues for events/stats to avoid RTP hot-path DB writes.
- Schema is in `init.sql` (sessions, events, payloads, stats, dialogs, trunks, session directory, gateway instances).
- When DB is disabled, `noopStore` keeps runtime behavior without persistence.

Operational checks after DB-related changes:

1. Verify startup can connect (`DB_ENABLE=true`, valid `DB_DSN`).
2. Verify event/payload insertion on call setup and teardown.
3. Verify partition maintenance worker creates future partitions.
4. Verify redirect tables (`session_directory`, `gateway_instances`) are updated and cleaned up.
5. Verify queue backpressure behavior does not impact call media loops.

---

## 10. Build, Run, Test

```bash
# Build
go build -o k2-gateway .

# Run
./k2-gateway

# Run tests
go test ./...

# SIP key formatting + SRV behavior tests
go test -v ./internal/sip -run "TestBuildPublicAccountKey|TestResolveSIPDestination"
```

If fixing a bug, prefer adding a focused `_test.go` reproduction in the affected package.

---

## 11. Troubleshooting Quick Guide

### 488 Not Acceptable Here

Most common causes:

1. malformed SDP `o=` username
2. codec mismatch (must support Opus + H.264)
3. video not enabled on SIP endpoint

Checkpoints:

- confirm SDP origin username fallback logic in `internal/sip/sdp.go`
- confirm peer config has `allow=opus` and `allow=h264`
- confirm `videosupport=yes`/equivalent endpoint settings

### Video rejected (`m=video ... 0`)

- inspect full SDP answer logs from `internal/sip/sdp.go`
- validate AVPF compatibility (`SIP_VIDEO_USE_AVPF` and endpoint support)
- temporarily force AVP with `SIP_FORCE_AVP=true` for interoperability testing

### Auth/register timeouts

- transport consistency matters; requests explicitly set transport to avoid digest retry switching transports.

### 401 Unauthorized on API/WS

Checkpoints:

- Verify `AUTH_ENABLE`, `AUTH_JWKS_URL`, `AUTH_JWT_ISSUER`, `AUTH_JWT_AUDIENCE` are set correctly.
- Verify token `iss` and `aud` match configured values exactly.
- Verify token is not expired (`exp`) and is already valid (`nbf`).
- Verify JWT header `kid` exists in current JWKS (gateway auto-refreshes JWKS once on unknown `kid`).
- For WebSocket, verify client sends `access_token` query param on the connect URL.

---

## 12. Agent Workflow Expectations

When modifying this codebase:

1. Start by tracing the exact call path (`api` -> `session` -> `sip` -> `logstore`).
2. Keep lock scopes narrow and avoid network calls inside critical sections.
3. Preserve wire compatibility for WebSocket and SIP unless change is explicitly requested.
4. Keep logs actionable; include session IDs in high-signal logs.
5. Validate with `go test ./...` after meaningful changes.

If a change touches media forwarding, SDP, or session lifecycle, perform an extra careful review for race and regression risk before finalizing.

---

## 13. LLM Context File (`llm.txt`)

- `llm.txt` is a compact, implementation-focused context file for external AI tools (Cursor, Windsurf, etc.).
- Use it as the first-read summary, then verify behavior in source files before editing hot paths.
- Keep `llm.txt` in sync when changing:
  - WebSocket/REST contracts
  - Session lifecycle/resume logic
  - SIP trunk/public auth flows
  - Media invariants (Opus passthrough, H.264 handling, SPS/PPS/keyframe logic)
  - Database schema or operational behavior
- Source of truth remains code + this `AGENTS.md`; `llm.txt` is a fast onboarding layer.

# K2 Gateway - AI Agent Development Guide

> **Role:** Senior Go Engineer / Systems Architect
> **Objective:** Keep a production-grade WebRTC <-> SIP bridge stable under real-time load.
> **Hard constraint:** No panics, no races, no media regressions.

---

## 1. Mission-Critical Rules

1. **Stability first.** This service is in the media path. A minor bug can become dropped calls, one-way audio, or black video.
2. **Concurrency discipline is mandatory.** Protect shared state with `sync.RWMutex`; do not hold locks while doing network I/O.
3. **Do not break media invariants.**
   - Audio is **Opus passthrough** by default; optional inbound gain (`SIP_AUDIO_INBOUND_GAIN_ENABLE`) transcodes SIP → WebRTC only.
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
- **Persistence:** Optional Postgres-backed LogStore (no-op when `DB_ENABLE=false`)
- **Push:** `internal/push/` — FCM, APNS, TTRS incoming-call dispatch
- **Translation:** `internal/translator/` — gRPC client for live caption/TTS
- **Audio processing:** `internal/audio/` — optional inbound Opus gain (CGO/libopus)
- **Mobile SIP provision:** `internal/sipclientauth/` — JWT-to-trunk registration client

---

## 3. Runtime Architecture

1. `main.go` loads env config, initializes logger, starts LogStore, and boots API mode.
2. `internal/auth/*` loads JWKS and verifies JWT (`iss`, `aud`, `exp/nbf`, RSA signature).
3. `internal/api/server.go` — `Server` struct, `NewServer`, route registration in `Start()`.
4. `internal/api/ws_*.go` — WebSocket upgrade, dispatch, and per-flow handlers.
5. `internal/api/handlers_*.go` — REST endpoints and SSE streams.
6. `internal/session/*` owns per-call state and WebRTC PeerConnection lifecycle.
7. `internal/sip/*` handles SIP signaling, SDP, public registry, and trunk manager.
8. `internal/logstore/*` persists events/payloads/stats/dialog/session snapshots.

---

## 4. Repository Map

> Client integration guides: `docs/dual-flow.md`, `docs/call-resume.md`, `docs/web.md`, `docs/react-native.md`, `docs/ios.md`, `docs/android.md`.

```text
apps/gateway/
|- main.go
|- internal/
|  |- api/           # HTTP/WS server (see file map below)
|  |- auth/verifier.go
|  |- audio/         # inbound Opus gain
|  |- config/config.go
|  |- logstore/
|  |- push/          # FCM, APNS, TTRS
|  |- session/       # WebRTC + RTP forwarding
|  |- sip/           # SIP server, trunks, SDP
|  |- sipclientauth/ # mobile trunk provision client
|  |- translator/    # gRPC translation client
|  `- webrtc/
|- init.sql
`- .env.example
```

---

## 5. internal/api/ File Map

| File | Responsibility |
|------|----------------|
| `server.go` | `Server`, interfaces, `WSClient`/`WSMessage`, `NewServer`, `Start`, routing |
| `auth_http.go` | HTTP/WS JWT middleware |
| `mobile_sip_provision.go` | Mobile trunk auto-provision on `/ws` connect |
| `client_diagnostics.go` | `/api/client-diagnostics` upload and query |
| `ws_conn.go` | WebSocket upgrade, read/write pumps (`/ws`, `/ws-public`, `/ws-agent`) |
| `ws_agent.go` | `/ws-agent` `agent_register`, refcount presence, last-disconnect hangup+unregister |
| `ws_dispatch.go` | `handleWSMessage` router, public-only guards |
| `ws_call.go` | `offer`, `ice`, `call`, `hangup`, `dtmf` |
| `ws_incoming.go` | `accept`, `reject`, push dispatch, ring timeout |
| `ws_resume.go` | `resume`, SDP diagnostics |
| `ws_trunk.go` | `trunk_resolve`, `trunk_push_token` |
| `ws_midcall.go` | `renegotiate_answer`, `client_state`, `request_keyframe`, `ping` |
| `ws_translate.go` | `translate`, `translate_stop`, caption events |
| `ws_notify.go` | `Notify*` callbacks, `send_message` |
| `ws_util.go` | `sendWSMessage`, logging helpers, SSE broadcast |
| `handlers.go` | Shared REST types, `respondJSON`/`respondError` |
| `handlers_call.go` | offer/call/hangup/dtmf/switch REST |
| `handlers_trunk.go` | trunk CRUD, register/unregister, heartbeat |
| `handlers_session.go` | session history, events, payloads, dialogs, stats |
| `handlers_ops.go` | dashboard, instances, ws-clients, public accounts |
| `handlers_sse.go` | trunk/session/ws-client SSE streams |
| `handlers_log.go` | `/api/logs/*` |

---

## 6. Where to Start

| Task | File(s) |
|------|---------|
| WS connect / auth / mobile provision | `ws_conn.go`, `auth_http.go`, `mobile_sip_provision.go` |
| PC agent WS register / presence | `ws_conn.go`, `ws_agent.go`, `sip/trunk_manager.go` (`UpsertAgentTrunk`) |
| WS message routing | `ws_dispatch.go` |
| Outbound call / offer / hangup | `ws_call.go` |
| Incoming call / accept / reject / push | `ws_incoming.go`, `internal/push/` |
| Session resume | `ws_resume.go` |
| Trunk resolve / push token | `ws_trunk.go` |
| Mid-call renegotiation | `ws_midcall.go`, `internal/session/renegotiate.go` |
| Live translation | `ws_translate.go`, `internal/translator/` |
| SIP MESSAGE / DTMF notify | `ws_notify.go` |
| REST call control | `handlers_call.go` |
| Trunk CRUD / register | `handlers_trunk.go`, `internal/sip/trunk_manager.go` |
| Session history / payloads | `handlers_session.go` |
| Dashboard / ops REST | `handlers_ops.go` |
| Log file API | `handlers_log.go` |
| Client diagnostics | `client_diagnostics.go` |
| SDP / codecs / RTP | `internal/sip/sdp.go`, `internal/session/session_media.go` |
| Inbound audio gain | `internal/audio/inbound.go` |

Full WS message types: [`docs/gateway/ws-contract.md`](../../docs/gateway/ws-contract.md).

---

## 7. Critical Media Behaviors

- **Audio:** Opus passthrough default; optional SIP→WebRTC gain in `internal/audio/` (`SIP_AUDIO_INBOUND_GAIN_ENABLE`). Outbound passthrough. DTMF via RFC2833.
- **Video:** H.264 only. Preserve SPS/PPS cache/reinject (`h264_paramsets.go`, `keyframe.go`, `sip/sdp.go`).
- **Recovery:** Session resume is first-class; session directory enables cross-instance redirect; incoming uses first-accept-wins.

---

## 8. Build, Run, Test

```bash
go build -o k2-gateway .
./k2-gateway
go test ./...
go test -v ./internal/sip -run "TestBuildPublicAccountKey|TestResolveSIPDestination"
```

Prefer focused `_test.go` reproductions in the affected package when fixing bugs.

---

## 9. Agent Workflow

When modifying this codebase:

1. Trace the exact call path (`api` → `session` → `sip` → `logstore`).
2. Keep lock scopes narrow; no network I/O inside critical sections.
3. Preserve wire compatibility unless change is explicitly requested.
4. Include session IDs in high-signal logs.
5. Run `go test ./...` after meaningful changes.

**Agent rules:**

1. Use the file map — do not read `server.go` wholesale for WS handler work.
2. WS message changes: update `ws_dispatch.go` + handler file + `docs/gateway/ws-contract.md`.
3. New files should stay ≤ ~600 lines; split further if exceeded.
4. Do not merge unrelated handlers back into monolith files.
5. Mechanical moves only — no behavior changes unless explicitly requested.

Extra review required for media forwarding, SDP, or session lifecycle changes.

---

## 10. Reference Docs

| Topic | Document |
|-------|----------|
| WebSocket contract | [`docs/gateway/ws-contract.md`](../../docs/gateway/ws-contract.md) |
| Environment variables | [`docs/gateway/config-reference.md`](../../docs/gateway/config-reference.md) |
| Logs, diagnostics, DB MCP | [`docs/gateway/ops-guide.md`](../../docs/gateway/ops-guide.md) |
| Troubleshooting | [`docs/gateway/troubleshooting.md`](../../docs/gateway/troubleshooting.md) |

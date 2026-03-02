<!-- Copilot instructions for AI coding agents in this repository -->

# Copilot instructions — webrtc-gateway

Purpose: short, actionable guidance to help an AI agent be immediately productive in this Nx monorepo.

1. Big picture

- Monorepo managed by Nx. Primary apps:
  - `gateway-frontend` — React + Vite UI (apps/gateway-frontend).
  - `gateway-sip` — Go WebRTC <-> SIP gateway (apps/gateway-sip).
  - `ttrs-vri-mobile` — Expo/React Native softphone (apps/ttrs-vri-mobile).
- Control plane: JSON over WebSocket/REST. Data plane: WebRTC SRTP ↔ RTP for media.

2. Where to start (key files)

- Backend entry: [apps/gateway-sip/main.go](apps/gateway-sip/main.go).
- Backend API/ws handlers: [apps/gateway-sip/internal/api/server.go](apps/gateway-sip/internal/api/server.go).
- Per-call lifecycle and media: [apps/gateway-sip/internal/session](apps/gateway-sip/internal/session).
- SIP behavior and SDP: [apps/gateway-sip/internal/sip](apps/gateway-sip/internal/sip).
- Frontend router & generated routes: [apps/gateway-frontend/src/routeTree.gen.ts](apps/gateway-frontend/src/routeTree.gen.ts).
- Frontend WS/contract handling: [apps/gateway-frontend/src/features/gateway/store/gateway-store.ts](apps/gateway-frontend/src/features/gateway/store/gateway-store.ts).

3. Critical developer workflows (commands)

- Inspect projects: `npx nx show projects`
- Frontend dev/build/test: `npx nx dev gateway-frontend`, `npx nx build gateway-frontend`, `npx nx test gateway-frontend`.
- SIP (Go) serve/build/test: `npx nx serve gateway-sip`, `npx nx build gateway-sip`, `npx nx test gateway-sip` or `go test ./...` inside the Go app.
- Use root npm scripts when convenient (see README). Prefer `npx nx` for focused targets.

4. Runtime architecture (backend)

- `main.go` loads env config, initializes logger and LogStore (optional), and starts API/WS.
- `internal/api/server.go` routes `/ws` messages and delegates to `internal/session` for lifecycle handling.
- `internal/session/*` manages PeerConnection lifecycle, RTP forwarding, SDP H.264 paramset handling and keyframe injection.
- `internal/sip/*` implements SIP dialog handling, trunk manager, public registry, and destination resolution.

5. Critical media behaviors (do not regress)

- Audio: Opus passthrough only (no transcoding). DTMF uses RFC2833 (`telephone-event`).
- Video: H.264 only — preserve SPS/PPS caching and keyframe injection (`internal/session/h264_paramsets.go`, `internal/session/keyframe.go`).
- Session resume: implemented via `session_directory` + `gateway_instances` for cross-instance redirect — changes require DB + runtime checks.

6. Project-specific conventions and guardrails

- Backend (Go): Go 1.25.5. Stability-first: avoid panics, protect shared state with `sync.RWMutex`, and never hold locks during network I/O. See [apps/gateway-sip/AGENTS.md](apps/gateway-sip/AGENTS.md).
- Frontend (TS): strict TypeScript, `@/` alias for `src/`, do not edit generated `src/routeTree.gen.ts`.
- Mobile: single-use WS connection model. See `apps/ttrs-vri-mobile/AGENTS.md` for run/build notes.

7. WebSocket contract (authoritative)

- Endpoint `/ws`. Message types (client→server): `offer`, `call`, `hangup`, `accept`, `reject`, `dtmf`, `send_message`, `resume`, `trunk_resolve`, `ping`.
- Server→client types: `answer`, `state`, `incoming`, `message`, `resumed`, `trunk_resolved`, `error`, `pong`.
- If you add/change a message type, update both: backend switch/payloads (`internal/api/server.go`) and frontend handlers (`src/features/gateway/store/gateway-store.ts`).

8. Testing and making safe changes

- Add focused unit tests in the affected package (Go: `_test.go` in the same package; Frontend: Vitest files under `src/`).
- After changes, run the minimal Nx target(s) first, then broader tests: `npx nx test <project>` → `npx nx build <project>`.

9. Configuration highlights (see `apps/gateway-sip/internal/config/config.go`)

- SIP bind/public IPs: `SIP_LOCAL_IP`, `SIP_PUBLIC_IP`, `SIP_PORT`.
- API toggles: `API_PORT`, `API_ENABLE_WS`, `API_ENABLE_REST`, `API_CORS_ORIGINS`.
- RTP range and tuning: `RTP_PORT_MIN`, `RTP_PORT_MAX`, `RTP_BUFFER_SIZE`.
- DB / LogStore: `DB_ENABLE`, `DB_DSN`, `DB_BATCH_SIZE`, `DB_RETENTION_*`.
- Trunk / multi-instance: `SIP_TRUNK_ENABLE`, `GATEWAY_INSTANCE_ID`, `GATEWAY_PUBLIC_WS_URL`, `SESSION_DIRECTORY_TTL_SECONDS`.

10. Stability & concurrency guardrails

- No panics in hot paths; prefer error logging and graceful degradation.
- Protect shared state with `sync.RWMutex`; do not hold locks while performing network I/O.
- Preserve media invariants — regressions here cause dropped calls or one-way audio.

11. When to escalate reviewer attention

- Any change touching media forwarding, SDP generation/parsing, keyframe logic, or SIP trunk registration must get a senior Go reviewer and extra runtime verification (no regressions allowed).

12. Quick debugging tips

- Check logs for session IDs, inspect DB partitions when persistence is enabled, reproduce with focused `go test` cases (see SIP tests in `internal/sip`).

If anything is unclear or you'd like this trimmed or expanded (more examples, checklist for PRs, or automated test commands), say which sections to iterate.

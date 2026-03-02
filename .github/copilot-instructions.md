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
- Use root npm scripts when convenient (see README). Always prefer `npx nx` for focused targets.

4. Project-specific conventions and guardrails

- Backend (Go): Go 1.25.5. Stability-first: avoid panics, protect shared state with `sync.RWMutex`, and never hold locks during network I/O. See [apps/gateway-sip/AGENTS.md](apps/gateway-sip/AGENTS.md).
- Media invariants: Opus audio passthrough, H.264 video only. Do not change SPS/PPS handling or keyframe injection logic (see `internal/session/h264_paramsets.go` and `internal/session/keyframe.go`).
- Frontend (TS): strict TypeScript, `@/` alias for `src/`, do not edit generated `src/routeTree.gen.ts`.
- Mobile: single-use WS connection model. See `apps/ttrs-vri-mobile/AGENTS.md` for run/build notes.

5. WebSocket contract (authoritative)

- Endpoint `/ws`. Message types (client→server): `offer`, `call`, `hangup`, `accept`, `reject`, `dtmf`, `send_message`, `resume`, `trunk_resolve`, `ping`.
- Server→client types: `answer`, `state`, `incoming`, `message`, `resumed`, `trunk_resolved`, `error`, `pong`.
- If you add/change a message type, update both: backend switch/payloads (`internal/api/server.go`) and frontend handlers (`src/features/gateway/store/gateway-store.ts`).

6. Testing and making safe changes

- Add focused unit tests in the affected package (Go: `_test.go` in the same package; Frontend: Vitest files under `src/`).
- After changes, run the minimal Nx target(s) first, then broader tests: `npx nx test <project>` → `npx nx build <project>`.

7. Configuration & env

- Primary env config in `apps/gateway-sip/internal/config/config.go` and `.env.example` at the app root. Critical toggles: `DB_ENABLE`, `GATEWAY_PUBLIC_WS_URL`, `SIP_*` and `RTP_*` ranges.

8. Integration points and cross-component patterns

- Trunk/public-id migration: code accepts both numeric `trunkId` and `trunkPublicId` (UUID); update both frontend and backend when touching trunk APIs.
- Session resume uses `session_directory` + `gateway_instances` for cross-instance redirect; changes require DB + runtime checks.

9. When to escalate reviewer attention

- Any change touching media forwarding, SDP generation/parsing, keyframe logic, or SIP trunk registration must get a senior Go reviewer and extra runtime verification (no regressions allowed).

10. Quick debugging tips

- Check logs for session IDs, inspect DB partitions when persistence is enabled, reproduce with focused `go test` cases (see SIP tests in `internal/sip`).

If anything is unclear or you'd like this trimmed or expanded (more examples, checklist for PRs, or automated test commands), say which sections to iterate.

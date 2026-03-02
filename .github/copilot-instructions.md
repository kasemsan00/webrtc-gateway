<!-- Copilot instructions for AI coding agents in this repository -->

# Copilot instructions — webrtc-gateway

## Big picture

- Nx monorepo with 3 active apps: `apps/gateway-sip` (Go WebRTC↔SIP gateway), `apps/gateway-frontend` (React/Vite operations UI), `apps/ttrs-vri-mobile` (Expo RN softphone).
- Control plane is JSON over WebSocket/REST; media plane is WebRTC SRTP on client side bridged to SIP RTP/RTCP.
- Runtime ownership: API/WebSocket in `internal/api`, per-call/media in `internal/session`, SIP/dialog/trunk logic in `internal/sip`.

## First files to read

- Backend entry and wiring: [apps/gateway-sip/main.go](apps/gateway-sip/main.go)
- WS contract + handlers: [apps/gateway-sip/internal/api/server.go](apps/gateway-sip/internal/api/server.go)
- Media invariants and recovery: [apps/gateway-sip/internal/session/keyframe.go](apps/gateway-sip/internal/session/keyframe.go), [apps/gateway-sip/internal/session/h264_paramsets.go](apps/gateway-sip/internal/session/h264_paramsets.go), [apps/gateway-sip/internal/session/renegotiate.go](apps/gateway-sip/internal/session/renegotiate.go)
- Frontend WS orchestration: [apps/gateway-frontend/src/features/gateway/store/gateway-store.ts](apps/gateway-frontend/src/features/gateway/store/gateway-store.ts)
- Mobile call/recovery flow: [apps/ttrs-vri-mobile/store/sip-store.ts](apps/ttrs-vri-mobile/store/sip-store.ts), [apps/ttrs-vri-mobile/lib/gateway/gateway-client.ts](apps/ttrs-vri-mobile/lib/gateway/gateway-client.ts)

## Critical workflows

- Discover projects/targets: `npx nx show projects`
- Backend: `npx nx serve gateway-sip`, `npx nx test gateway-sip`, `npx nx build gateway-sip`
- Frontend: `npx nx dev gateway-frontend`, `npx nx test gateway-frontend`, `npx nx build gateway-frontend`
- Mobile: `npx nx dev ttrs-vri-mobile`, `npx nx lint ttrs-vri-mobile` (no Nx `test` target currently)
- Use focused Nx targets first; avoid running toolchains directly unless target is missing.

## Project-specific guardrails

- Gateway is stability-first: no panics in hot paths; protect shared mutable state with `sync.RWMutex`; never hold locks during network I/O.
- Media invariants are product behavior: audio Opus passthrough, video H.264 only, preserve SPS/PPS caching + keyframe recovery logic.
- Frontend/mobile TypeScript uses strict mode and `@/` alias; do not hand-edit generated [apps/gateway-frontend/src/routeTree.gen.ts](apps/gateway-frontend/src/routeTree.gen.ts).

## WebSocket contract sync rules

- Endpoint is `/ws`; contract changes are cross-app changes.
- Client→server includes: `offer`, `call`, `hangup`, `accept`, `reject`, `dtmf`, `send_message`, `resume`, `request_keyframe`, `trunk_resolve`, `ping`.
- Server→client includes: `answer`, `state`, `incoming`, `message`, `messageSent`, `resumed`, `resume_failed`, `resume_redirect`, `trunk_resolved`, `trunk_redirect`, `trunk_not_found`, `trunk_not_ready`, `pong`, `error`.
- If message schema/type changes, update all three: backend handler switch (`server.go`), frontend store handler (`gateway-store.ts`), mobile message types/client (`lib/gateway/types.ts`, `gateway-client.ts`).

## Integration/config highlights

- Config source of truth: [apps/gateway-sip/internal/config/config.go](apps/gateway-sip/internal/config/config.go)
- Multi-instance resume/redirect depends on DB-backed session directory (`GATEWAY_INSTANCE_ID`, `GATEWAY_PUBLIC_WS_URL`, `SESSION_DIRECTORY_TTL_SECONDS`).
- Trunk/public identity behavior is tightly coupled to SIP + DB state; validate end-to-end when changing registration, destination resolution, or resume paths.

## Safe change checklist

- Keep changes small and local to affected app/package.
- For backend/media changes, run `npx nx test gateway-sip`.
- For frontend contract/UI changes, run `npx nx test gateway-frontend` and note any environment-only dependency issues.
- For mobile changes, run lint/type-check in app folder and validate foreground/reconnect call flows manually.

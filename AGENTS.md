# Project Guidelines

## Hierarchy

This file defines root defaults. App-level rules take precedence:
- `apps/frontend/AGENTS.md`
- `apps/gateway/AGENTS.md`

## Commands

Run from repo root unless working in a specific app directory.

- `pnpm install` — install all deps (requires pnpm@10, Node >= 18)
- `pnpm build` — build all workspaces
- `pnpm lint` — lint all workspaces
- `pnpm check-types` — type-check all workspaces
- `pnpm format` — format with Prettier
- `pnpm dev:frontend` — frontend dev (port 3150)
- `pnpm dev:backend` — runs `go run .` in `apps/gateway`

Go commands (run from `apps/gateway`):
- `go test ./...`
- `go build -o webrtc-sip-gateway .`
- `go test -v ./internal/sip -run "TestBuildPublicAccountKey|TestResolveSIPDestination"`

Frontend single-test (from root):
- `pnpm --filter frontend run test -- src/features/trunk/types.test.ts`
- `pnpm --filter frontend run test -- -t "test name pattern"`

## Environment

Copy per-app env examples before running:
- `apps/frontend/.env.example` → `.env`
- `apps/gateway/.env.example` → `.env`

Gateway env is loaded via `godotenv`; `AUTH_ENABLE` triggers fail-fast startup checks for JWKS, issuer, and audience.

## Architecture

- Monorepo: `pnpm` workspaces (`apps/*`, `packages/*`) + Turborepo (`turbo.json`)
- `apps/frontend`: React + TypeScript + **TanStack Start** (SSR via Vite, port 3150)
- `apps/gateway`: Go WebRTC↔SIP bridge, module path `webrtc-sip-gateway`, Go 1.26.2
- `packages/`: `eslint-config`, `typescript-config`, `ui`
- `pnpm-workspace.yaml` blocks native builds (`esbuild`, `sharp`, etc.) on virtiofs mounts

## Critical Rules

- **Never edit generated files:** `apps/frontend/src/routeTree.gen.ts`
- **SDP/media/SIP changes in `apps/gateway` are high risk** — invariants: Opus audio passthrough (no transcoding), H.264 video only, SPS/PPS caching and keyframe injection preserved
- **Pion (`github.com/pion/webrtc/v4`) is the WebRTC stack** — do not replace or mix stacks without explicit request
- **Keep WS/API contract compatibility** across mobile clients; if you add/change a message type, update `internal/api/server.go` and `docs/gateway/ws-contract.md`
- **Trunks are soft-deleted** (`enabled=false`), never hard-deleted from `sip_trunks`
- **No panics in hot paths** — RTP/RTCP/SIP loops should log and continue
- **Avoid cross-app refactors** — scope changes to the target app or package

## Testing

- Frontend: Vitest with jsdom; config is in `vite.config.ts` (no separate vitest config file)
- Backend: `go test ./...`; fix bugs by adding a focused `_test.go` in the affected package
- No CI workflows exist (`.github/workflows/` is empty)

## Key References

- Frontend guide: `apps/frontend/AGENTS.md`
- Gateway guide + WebSocket contract + config reference: `apps/gateway/AGENTS.md`
- Dual-flow architecture: `docs/dual-flow.md`
- Call resume behavior: `docs/call-resume.md`
- Client integration guides: `docs/web.md`, `docs/react-native.md`, `docs/ios.md`, `docs/android.md`
- Docker CI build script: `docker-ci.ps1`
- Workspace layout: `pnpm-workspace.yaml`

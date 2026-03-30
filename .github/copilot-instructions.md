# Copilot Instructions - webrtc-gateway

## Purpose

Give AI agents a fast, safe starting point for this monorepo. Keep instructions short here and link to the authoritative docs for details.

## Scope and hierarchy

- This file is root-level bootstrap guidance for the full monorepo.
- App-specific rules take precedence:
	- `apps/frontend/AGENTS.md`
	- `apps/gateway/AGENTS.md`

## Start here

Run from repo root unless a task explicitly targets an app folder.

- Install: `pnpm install`
- Full workspace: `pnpm build`, `pnpm lint`, `pnpm check-types`, `pnpm dev`
- Frontend dev: `pnpm dev:frontend`
- Backend dev: `pnpm dev:backend`
- Backend tests: `Set-Location apps/gateway; go test ./...`

## Repository map

- `apps/frontend`: React + TypeScript + Vite operations UI.
- `apps/gateway`: Go WebRTC <-> SIP bridge.
- `packages/*`: shared lint, TS config, and UI packages.

## Guardrails

- Do not edit generated files such as `apps/frontend/src/routeTree.gen.ts`.
- Changes in `apps/gateway` media/SDP/SIP paths are high risk; preserve behavior unless task requires protocol changes.
- Keep API/WS contract compatibility across clients when changing signaling payloads.
- Prefer scoped changes in one app/package; avoid cross-repo refactors unless required.

## Environment setup

- Configure app env files before running local dev:
	- `apps/frontend/.env.example`
	- `apps/gateway/.env.example`

## Link-first references

- Root project guidance: `AGENTS.md`
- Frontend implementation details: `apps/frontend/AGENTS.md`
- Frontend runtime/env behavior: `apps/frontend/README.md`
- Gateway implementation details: `apps/gateway/AGENTS.md`
- Gateway architecture and flows: `apps/gateway/docs/dual-flow.md`
- Gateway reconnect behavior: `apps/gateway/docs/call-resume.md`

## Example prompts

- "Run frontend and backend locally, then list missing env vars."
- "Add a typed WebSocket handler in `apps/gateway/internal/api` and include tests."
- "Refactor `apps/frontend/src/lib/http-client.ts` without changing behavior and run tests."

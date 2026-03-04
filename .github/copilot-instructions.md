<!-- markdownlint-disable -->
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

## Workspace execution rules ⚙️

These guidelines are pulled from the monorepo’s `AGENTS.md` and apply to all AI agents interacting with the code.

- Prefer Nx commands from the workspace root for all tasks.
- Use `npx nx ...` (or root npm scripts) instead of calling local toolchains directly.
- Prefer focused targets (`nx run <project>:<target>`) over global commands.
- Before assumptions, inspect the project list with `npx nx show projects`.

### Common Nx commands

- Show projects: `npx nx show projects`
- Frontend:
  - `npx nx dev gateway-frontend`
  - `npx nx build gateway-frontend`
  - `npx nx test gateway-frontend`
  - `npx nx lint gateway-frontend`
- SIP gateway:
  - `npx nx serve gateway-sip`
  - `npx nx build gateway-sip`
  - `npx nx test gateway-sip`

### Nx configuration guardrails

- Prefer explicit targets in `project.json`.
- Avoid reintroducing fragile plugin inference unless there is a clear reason.
- Keep root scripts in `package.json` aligned with real Nx targets.

### Helpful skills and workflows

- For navigating/exploring the workspace, invoke the `nx-workspace` skill first; it has patterns for querying projects, targets, and dependencies.
- When running tasks (build, test, lint, serve, etc.), always prefer running them through `nx` (i.e. `nx run`, `nx run-many`, `nx affected`) instead of using the underlying tooling directly.
- Prefix nx commands with the workspace's package manager (e.g., `pnpm nx build`, `npm exec nx test`) to avoid relying on globally installed CLIs.
- For scaffolding tasks (apps, libs, project structure, setup), use the `nx-generate` skill before exploring or calling docs.
- For plugin discovery or adding new tech support, the `nx-plugins` skill is your go‑to.
- When uncertain about flags or options, consult `nx_docs` or the built-in help; never guess.

### Skills available in this session 🧠

- `skill-creator` — create or update skills (see `C:/Users/Kasemsan/.codex/skills/.system/skill-creator/SKILL.md`).
- `skill-installer` — install new skills from curated lists or GitHub (see `C:/Users/Kasemsan/.codex/skills/.system/skill-installer/SKILL.md`).

Skill usage policy mirrors the policies stated in `AGENTS.md`:

- Use a listed skill when explicitly named or when the request clearly matches its description.
- If a named skill isn’t available, note that and proceed with the best fallback.
- Read minimal necessary sections of a skill file and prefer running provided scripts/assets when relevant.

<!-- nx configuration start-->
<!-- Leave the start & end comments to automatically receive updates. -->

## General Guidelines for working with Nx

- For navigating/exploring the workspace, invoke the `nx-workspace` skill first - it has patterns for querying projects, targets, and dependencies
- When running tasks (for example build, lint, test, e2e, etc.), always prefer running the task through `nx` (i.e. `nx run`, `nx run-many`, `nx affected`) instead of using the underlying tooling directly
- Prefix nx commands with the workspace's package manager (e.g., `pnpm nx build`, `npm exec nx test`) - avoids using globally installed CLI
- You have access to the Nx MCP server and its tools, use them to help the user
- For Nx plugin best practices, check `node_modules/@nx/<plugin>/PLUGIN.md`. Not all plugins have this file - proceed without it if unavailable.
- NEVER guess CLI flags - always check nx_docs or `--help` first when unsure

## Scaffolding & Generators

- For scaffolding tasks (creating apps, libs, project structure, setup), ALWAYS invoke the `nx-generate` skill FIRST before exploring or calling MCP tools

## When to use nx_docs

- USE for: advanced config options, unfamiliar flags, migration guides, plugin configuration, edge cases
- DON'T USE for: basic generator syntax (`nx g @nx/react:app`), standard commands, things you already know
- The `nx-generate` skill handles generator discovery internally - don't call nx_docs just to look up generator syntax

<!-- nx configuration end-->

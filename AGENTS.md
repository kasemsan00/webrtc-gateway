# Project Guidelines

## Scope And Hierarchy

- This file defines root defaults for the monorepo.
- More specific rules in app folders override these defaults:
  - `apps/gateway-frontend/AGENTS.md`
  - `apps/gateway-sip/AGENTS.md`

## Architecture

- Monorepo tooling:
  - `pnpm` workspaces (`apps/*`, `packages/*`)
  - Turborepo task orchestration (`turbo.json`)
- Main applications:
  - `apps/gateway-frontend`: React + TypeScript + Vite operations UI.
  - `apps/gateway-sip`: Go WebRTC <-> SIP gateway service.
- Shared packages:
  - `packages/eslint-config`
  - `packages/typescript-config`
  - `packages/ui`

## Build And Test

- Install dependencies from repo root:
  - `pnpm install`
- Run all workspace tasks:
  - `pnpm build`
  - `pnpm lint`
  - `pnpm check-types`
  - `pnpm dev`
- Run focused app development:
  - Frontend: `pnpm dev:frontend`
  - Backend: `pnpm exec turbo dev --filter=gateway-sip`
- App-specific commands:
  - Frontend commands are in `apps/gateway-frontend/package.json`.
  - Backend commands are in `apps/gateway-sip/project.json`.

## Conventions

- Keep changes scoped to the target app or package; avoid cross-app refactors unless required.
- Prefer strict typing and explicit data-shape handling at API boundaries.
- Follow existing formatter/linter config rather than introducing new style rules.
- Do not manually edit generated files such as `apps/gateway-frontend/src/routeTree.gen.ts`.

## Pitfalls

- Root script `dev:backend` currently points to `gateway-backend`, which does not match the current project name. Use `pnpm exec turbo dev --filter=gateway-sip`.
- Frontend and backend require app-level environment setup before running. Check:
  - `apps/gateway-frontend/README.md`
  - `apps/gateway-sip/.env.example`
- Media-path changes in `apps/gateway-sip` are high risk. Preserve behavior unless the task explicitly requires protocol/media changes.

## Key References

- Monorepo tasks: `turbo.json`
- Workspace layout: `pnpm-workspace.yaml`
- Root scripts: `package.json`
- Frontend implementation guide: `apps/gateway-frontend/AGENTS.md`
- Backend implementation guide: `apps/gateway-sip/AGENTS.md`

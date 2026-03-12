# Project Guidelines

## Scope And Hierarchy

- This file defines root defaults for the monorepo.
- More specific rules in app folders override these defaults:
  - `apps/frontend/AGENTS.md`
  - `apps/gateway/AGENTS.md`

## Architecture

- Monorepo tooling:
  - `pnpm` workspaces (`apps/*`, `packages/*`)
  - Turborepo task orchestration (`turbo.json`)
- Main applications:
  - `apps/frontend`: React + TypeScript + Vite operations UI.
  - `apps/gateway`: Go WebRTC <-> SIP gateway service.
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
  - Backend: `pnpm dev:backend`
- App-specific commands:
  - Frontend commands are in `apps/frontend/package.json`.
  - Backend commands are in `apps/gateway/project.json`.

## Conventions

- Keep changes scoped to the target app or package; avoid cross-app refactors unless required.
- Prefer strict typing and explicit data-shape handling at API boundaries.
- Follow existing formatter/linter config rather than introducing new style rules.
- Do not manually edit generated files such as `apps/frontend/src/routeTree.gen.ts`.

## Pitfalls

- Frontend and backend require app-level environment setup before running. Check:
  - `apps/frontend/README.md`
  - `apps/gateway/.env.example`
- Media-path changes in `apps/gateway` are high risk. Preserve behavior unless the task explicitly requires protocol/media changes.

## Key References

- Monorepo tasks: `turbo.json`
- Workspace layout: `pnpm-workspace.yaml`
- Root scripts: `package.json`
- Frontend implementation guide: `apps/frontend/AGENTS.md`
- Backend implementation guide: `apps/gateway/AGENTS.md`

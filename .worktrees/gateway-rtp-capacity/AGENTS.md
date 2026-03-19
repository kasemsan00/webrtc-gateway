# Project Guidelines

## **Copilot / AI Agent Instructions**

- **Purpose:** Provide concise, repo-specific guidance for AI assistants and contributors.
- **Where to run:** Prefer running workspace commands from the repo root unless an app README instructs otherwise.
- **Essential commands:**
  - Install: `pnpm install`
  - Full workspace: `pnpm build`, `pnpm lint`, `pnpm check-types`, `pnpm dev`
  - Frontend dev: `pnpm dev:frontend` (or `pnpm --filter frontend run dev`)
  - Backend dev: `pnpm dev:backend` (or run `go` commands in `apps/gateway` / use `project.json`)
- **Environments:** Copy per-app env examples before running: `apps/frontend/.env.example`, `apps/gateway/.env.example`.
- **Don't edit generated files:** e.g. `apps/frontend/src/routeTree.gen.ts`.
- **High-risk areas:** Changes to SDP/media/SIP in `apps/gateway` are high risk — require tests and design review.
- **DB lifecycle rules:** Follow app guides for entities with soft-delete or managed lifecycles (e.g., SIP trunks).

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

## **Example Prompts for AI Agents**

- _Start a local dev environment:_ "Run the recommended steps to start the frontend and backend locally (install, envs, dev servers) and list any missing env vars."
- _Code change + tests:_ "Add a new typed API handler in `apps/gateway/internal/api` for X, include unit tests, and update docs."
- _Safe refactor:_ "Refactor `apps/frontend/src/lib/http-client.ts` to use a shared fetch wrapper, preserve behavior and add unit tests."
- _Investigate bug:_ "Search for occurrences of 'routeTree.gen.ts' edits and report where generated files were modified manually."

## **Suggested Agent Customizations**

- Create `.github/copilot-instructions.md` (applyTo: root) with a brief excerpt of this section for GitHub Copilot to read in PRs.
- Create `apps/frontend/.agent.md` that documents the Vite dev flow and lists generated files to ignore.
- Create `apps/gateway/.agent.md` that highlights SDP/SIP hotspots and database lifecycle constraints.

If you want, I can (1) create the `.github/copilot-instructions.md` file with a minimal excerpt, or (2) scaffold the per-app `.agent.md` files now. Which would you like me to create?

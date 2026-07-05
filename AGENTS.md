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

## Cursor Cloud specific instructions

Durable, non-obvious notes for running this repo in the Cursor Cloud VM. Standard commands live in the sections above and in `apps/frontend/package.json` / `apps/gateway/AGENTS.md`; this section only captures gotchas. The startup update script already runs `pnpm install` and pre-fetches Go modules.

### Toolchain
- `apps/gateway/go.mod` pins `go 1.25.5`. The VM's base `go` may be older, but `GOTOOLCHAIN=auto` (the default) transparently downloads and uses `go1.25.5` for `go run`/`go build`/`go test`. No manual Go upgrade is needed.
- Node 22 + `pnpm@10` (pinned via root `packageManager`) are used; `pnpm install` from the repo root installs all workspaces.

### Environment files (create before running; git-ignored, not present on a fresh clone)
- `cp apps/frontend/.env.example apps/frontend/.env`
- `cp apps/gateway/.env.example apps/gateway/.env`
- The gateway `.env.example` ships `DB_DSN=postgres://user:pass@localhost:5432/k2gateway?sslmode=disable` (a placeholder). Point it at the local DB, e.g. `postgres://k2user:k2pass@localhost:5432/k2_gateway?sslmode=disable`.

### Running the gateway (`apps/gateway`)
- `pnpm dev:backend` is effectively a no-op — `apps/gateway/package.json` has no `dev` script. Run the gateway directly: `cd apps/gateway && go run .` (API on `:8080`, WS `/ws`, REST `/api/*`, SIP `:5060`, RTP UDP `10500-10600`).
- The shipped `.env` has `DB_ENABLE=true` + `SIP_TRUNK_ENABLE=true`, so the gateway **fatal-exits on startup if Postgres is unreachable**. Two options:
  - Full setup (recommended, needed for trunk routing / logging / browser-to-browser): run PostgreSQL. Postgres 16 is installed in the VM image. Start it and provision once:
    - `sudo pg_ctlcluster 16 main start`
    - DB/user matching `docker-compose.yml`: role `k2user`/`k2pass`, database `k2_gateway` owned by `k2user`.
    - Load schema **as `k2user`** so dynamically-created monthly partitions work: `PGPASSWORD=k2pass psql -h localhost -U k2user -d k2_gateway -f apps/gateway/init.sql`.
  - No-DB quick run: set `DB_ENABLE=false` and `SIP_TRUNK_ENABLE=false` in `apps/gateway/.env`.
- SIP registration + TURN in `.env.example` point at external hosts (`turn.ttrs.or.th`, a hardcoded `SIP_PUBLIC_IP`). These are unreachable from the VM but are non-fatal — the gateway logs warnings and keeps serving the API/WS.

### Running the frontend (`apps/frontend`)
- `pnpm dev:frontend` serves on `:3150`.
- The entire UI is gated behind an **external Keycloak IdP** (`accounts.ttrs.or.th`, TTRS). On load the app redirects to that login page; without valid TTRS credentials you cannot reach the console UI. The dev server itself runs fine and the redirect confirms the auth flow is wired. To exercise UI flows you need real TTRS credentials (not available by default).

### Lint caveat
- `pnpm lint` does not pass on a clean checkout: `eslint` is only a peer of `@tanstack/eslint-config` (not linked into package `.bin`), and the committed frontend code currently has pre-existing lint errors. `pnpm build`, `pnpm check-types`, `pnpm test` (frontend, 88 tests) and `go test ./...` all pass.

### Quick end-to-end sanity check (no browser/login required)
With the gateway + Postgres running, the DB-backed core can be exercised via REST:
- `curl -s -X POST http://localhost:8080/api/trunks -H 'Content-Type: application/json' -d '{"name":"hello","domain":"sip.example.test","username":"u","password":"p","transport":"tcp"}'`
- `curl -s http://localhost:8080/api/trunks` — the created trunk persists in the `sip_trunks` table.

# Copilot / AI Assistant Instructions (excerpt)

**Purpose:** Brief, repo-specific guidance for AI assistants and contributors.

**Where to run:** Prefer running workspace commands from the repo root unless an app README instructs otherwise.

**Essential commands:**

- Install: `pnpm install`
- Full workspace: `pnpm build`, `pnpm lint`, `pnpm check-types`, `pnpm dev`
- Frontend dev: `pnpm dev:frontend` (or `pnpm --filter frontend run dev`)
- Backend dev: `pnpm dev:backend` (or use `go` commands in `apps/gateway` / `project.json`)

**Environment:** Copy per-app env examples before running: `apps/frontend/.env.example`, `apps/gateway/.env.example`.

**Do not edit generated files:** e.g., `apps/frontend/src/routeTree.gen.ts` is generated — preserve it.

**High-risk areas:** Changes to SDP/media/SIP in `apps/gateway` are high risk — require tests and design review.

**Example prompts:**

- "Start the frontend and backend locally and list any missing env vars."
- "Add a new typed API handler in `apps/gateway/internal/api` with unit tests."

If you want, I can also scaffold `apps/frontend/.agent.md` and `apps/gateway/.agent.md` with app-specific guidance. Please tell me which to create next.

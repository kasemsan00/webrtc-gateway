# AGENTS.md

## Workspace Overview

- Monorepo managed by Nx.
- Main apps:
  - `gateway-frontend`: React + Vite + TanStack Start app in `apps/gateway-frontend`.
  - `gateway-sip`: Go WebRTC/SIP gateway in `apps/gateway-sip`.
- Nx project configs are explicit:
  - `apps/gateway-frontend/project.json`
  - `apps/gateway-sip/project.json`

## Default Execution Rules

- Prefer Nx commands from workspace root for all tasks.
- Prefer running through package manager wrappers:
  - `npm exec nx ...` (preferred)
  - `npx nx ...` (acceptable fallback)
- Prefer focused targets (`nx run <project>:<target>`) over global commands.
- Before assumptions, inspect the current project list:
  - `npm exec nx show projects`

## Common Commands

- Show projects: `npm exec nx show projects`
- Frontend:
  - `npm exec nx run gateway-frontend:dev`
  - `npm exec nx run gateway-frontend:build`
  - `npm exec nx run gateway-frontend:test`
  - `npm exec nx run gateway-frontend:lint`
- SIP gateway:
  - `npm exec nx run gateway-sip:serve`
  - `npm exec nx run gateway-sip:build`
  - `npm exec nx run gateway-sip:test`

## Change Workflow

1. Identify affected app(s) and files.
2. Update code and config with minimal blast radius.
3. Run only relevant Nx targets first.
4. Run broader validation only when needed.
5. Summarize what changed and what was verified.

## Frontend Guardrails (`gateway-frontend`)

- Treat generated files as generated; do not hand-edit unless intentional.
- Keep route and API contract changes aligned with backend message/schema changes.
- Keep strict TypeScript and lint clean for touched files.
- If build/lint fails due to missing deps, install at workspace root and re-run through Nx.

## Backend Guardrails (`gateway-sip`)

- Preserve media invariants and SIP/WebRTC behavior unless explicitly requested.
- Prefer small, targeted Go changes; keep concurrency and error handling conservative.
- Validate with `npm exec nx run gateway-sip:test` for behavior changes.

## Nx Config Guardrails

- Prefer explicit targets in `project.json`.
- Avoid reintroducing fragile plugin inference unless there is a clear reason.
- Keep root scripts in `package.json` aligned with real Nx targets.

## Skills Available In This Session

- `skill-creator`: `C:/Users/Kasemsan/.codex/skills/.system/skill-creator/SKILL.md`
- `skill-installer`: `C:/Users/Kasemsan/.codex/skills/.system/skill-installer/SKILL.md`

## Skill Usage Policy

- Use a listed skill when the user explicitly names it or the request clearly matches it.
- If multiple skills apply, use the minimal set and state the order.
- If a named skill is unavailable, say so briefly and continue with best fallback.
- Read only the minimum needed from skill files and linked references.
- Prefer referenced scripts/assets/templates over re-creating content.
- Keep context small: avoid bulk-loading reference folders.

## Skills

A skill is a set of local instructions in a `SKILL.md` file.

### Available Skills

- `skill-creator`: Guide for creating effective skills. Use when users want to create or update a skill that extends Codex capabilities with specialized workflows or tool integrations.  
  File: `C:/Users/Kasemsan/.codex/skills/.system/skill-creator/SKILL.md`
- `skill-installer`: Install Codex skills into `$CODEX_HOME/skills` from curated lists or GitHub repo paths.  
  File: `C:/Users/Kasemsan/.codex/skills/.system/skill-installer/SKILL.md`

### How To Use Skills

- Discovery: use the list above as the authoritative in-session skill list.
- Trigger rules: if user names a skill (for example `$SkillName` or plain text), or the task clearly matches a listed skill, use it for that turn.
- Missing/blocked: if a named skill is not listed or cannot be read, say so briefly and continue with best fallback.
- Progressive disclosure:
  1. Open `SKILL.md` and read only enough to execute.
  2. Resolve relative paths against the skill directory first.
  3. Load only specific reference files needed for the task.
  4. Prefer using skill scripts and assets when available.
- Coordination:
  - If multiple skills apply, pick the minimal set and state execution order.
  - Briefly announce which skill(s) are being used and why.
- Safety/fallback: if skill instructions are unclear or incomplete, state the issue and continue with the next-best approach.

<!-- nx configuration start-->
<!-- Leave the start & end comments to automatically receive updates. -->

## General Guidelines For Working With Nx

- For workspace navigation/exploration, invoke the `nx-workspace` skill first.
- When running tasks (build, lint, test, e2e, and similar), run through `nx` (`nx run`, `nx run-many`, `nx affected`) instead of underlying tools directly.
- Prefix Nx commands with the workspace package manager wrapper (for example `pnpm nx ...` or `npm exec nx ...`) to avoid global CLI drift.
- Use the Nx MCP server and tools when available.
- For Nx plugin best practices, check `node_modules/@nx/<plugin>/PLUGIN.md` when it exists.
- Never guess CLI flags; check `nx_docs` or `--help` when unsure.

## Scaffolding & Generators

- For scaffolding tasks (apps, libs, project structure, setup), invoke the `nx-generate` skill first before exploring or calling MCP tools.

## When To Use `nx_docs`

- Use for advanced config options, unfamiliar flags, migration guides, plugin configuration, and edge cases.
- Do not use for basic generator syntax or standard commands you already know.
- `nx-generate` handles generator discovery internally; do not call `nx_docs` just to look up generator syntax.

<!-- nx configuration end-->

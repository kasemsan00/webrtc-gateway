# AGENTS.md - webrtc-gateway frontend

## Overview

React + TypeScript + TanStack Start frontend for gateway operations UI inside the `webrtc-gateway` pnpm monorepo.

## Project Structure

- `src/routes/`: TanStack file-based routes (`index.tsx`, `trunks.tsx`, `instances.tsx`, `sessions*.tsx`, `session-directory.tsx`).
- `src/features/`: domain modules (`gateway`, `gateway-instances`, `trunk`, `session-history`, `session-detail`, `session-directory`).
- `src/components/` and `src/components/ui/`: shared UI and primitives.
- `src/lib/`: shared utilities, store helpers, theme/provider utilities.
- Generated file: `src/routeTree.gen.ts` (do not edit manually).

## Commands

Run from `E:\dev\webrtc-gateway` unless a command explicitly says it is frontend-local.

- Install: `pnpm install`
- Dev: `pnpm dev:frontend` (port `3150`)
- Build: `pnpm --filter frontend run build`
- Preview: `pnpm --filter frontend run preview`
- Lint: `pnpm --filter frontend run lint`
- Format all workspace TS/TSX/MD: `pnpm format`
- Fix frontend format + lint: `pnpm --filter frontend run check`
- Tests: `pnpm --filter frontend run test`

Single-test examples:

- `pnpm --filter frontend run test -- src/features/trunk/types.test.ts`
- `pnpm --filter frontend run test -- -t "isRegisterActionDisabled" src/features/trunk/components/trunk-list-page.test.ts`
- `pnpm --filter frontend exec vitest run src/features/gateway/config.test.ts -t "normalizes gateway URL"`

## Conventions

- TypeScript strict mode is enabled.
- Prettier style: no semicolons, single quotes, trailing commas.
- Use `import type` for type-only imports.
- Import grouping: external, `@/` alias, relative.
- Prefer `@/*` alias for `src/*` paths.
- Avoid `any`; validate untyped API/WS payloads at boundaries.

## API/Contract Notes

- Keep trunk API compatibility for both `trunkId` (numeric) and `trunkPublicId` (UUID).
- For `/api/trunks`, server-side sorting does not support `activeCallCount`; sort that field client-side only.
- If backend WS/REST contracts change, update this app and affected mobile clients together.
- Active Sessions supports admin REST hangup/DTMF; session detail includes a Client diagnostics tab (mobile uploads).

## Cursor Rule

`.cursorrules` requires latest Shadcn when adding components (example: `pnpm dlx shadcn@latest add button`).

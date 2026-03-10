# AGENTS.md - webrtc-gateway-react

## Overview
React + TypeScript + Vite frontend for gateway operations UI.

## Project Structure
- `src/routes/`: TanStack file-based routes (`index.tsx`, `trunks.tsx`, `instances.tsx`, `sessions*.tsx`, `session-directory.tsx`).
- `src/features/`: domain modules (`gateway`, `gateway-instances`, `trunk`, `session-history`, `session-detail`, `session-directory`).
- `src/components/` and `src/components/ui/`: shared UI and primitives.
- `src/lib/`: shared utilities, store helpers, theme/provider utilities.
- Generated file: `src/routeTree.gen.ts` (do not edit manually).

## Commands
Run in `E:\dev\webrtc\webrtc-gateway-react`.

- Install: `npm install`
- Dev: `npm run dev` (port `3150`)
- Build: `npm run build`
- Preview: `npm run preview`
- Lint: `npm run lint`
- Format: `npm run format -- --write .`
- Fix format + lint: `npm run check`
- Tests: `npm run test`

Single-test examples:
- `npm run test -- src/features/trunk/types.test.ts`
- `npm run test -- -t "isRegisterActionDisabled" src/features/trunk/components/trunk-list-page.test.ts`
- `npx vitest run src/features/gateway/config.test.ts -t "normalizes gateway URL"`

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

## Cursor Rule
`.cursorrules` requires latest Shadcn when adding components (example: `pnpm dlx shadcn@latest add button`).

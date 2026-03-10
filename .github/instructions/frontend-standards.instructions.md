---
description: "Use when building React components, managing TanStack Router, using @tanstack/react-store, or writing tests in gateway-frontend. Covers strict typing, feature structure, API contracts, and Vitest patterns."
applyTo: "apps/gateway-frontend/**"
---

# Frontend Standards (gateway-frontend)

## Type Safety & API Boundaries

- **Strict TypeScript mode is enabled** — no `any` type, no unchecked indexed access.
- **Validate external data at boundaries** — all API responses and WebSocket messages must be validated before use.
  - Example: Use `normalizeTrunkUid()` in `src/features/trunk/types.ts` to handle dual ID support (`public_id` UUID vs `trunkId` numeric).
- **Use `import type`** for type-only imports to reduce bundle size.
- **Group imports**: external → `@/` alias → relative paths.

## Router & State Management

- **File-based routing**: TanStack Router generates `src/routeTree.gen.ts` automatically — **never edit it manually**.
- **Feature structure**: Organize by domain in `src/features/{module}/` with `types.ts`, `services/`, `store/`, `components/`.
  - Each feature owns its domain types and normalization logic.
- **State persistence**: Use `@tanstack/react-store` + localStorage helpers in `src/lib/store-persist.ts`.
- **API client**: Use `src/lib/http-client.ts` for Bearer token headers and `VITE_GATEWAY_URL` resolution.

## API Contract Notes

- **Dual ID support**: Keep trunk API compatibility for both `trunkId` (numeric DB key) and `trunkPublicId` (UUID external).
  - Frontend queries always use `trunkPublicId`; backend may return both for compatibility window.
- **Client-side sorting**: The `/api/trunks` endpoint does **not** support server-side sorting by `activeCallCount` — sort client-side only.
- **WebSocket stability**: Backend and mobile clients must align on message types and field names (breaking changes affect all clients).

## Testing Conventions

- **Collocate tests**: `file.test.ts` next to source, smoke tests as `*.smoke.test.ts` under `src/features/smoke/`.
- **Vitest setup**:
  - Use `// @vitest-environment jsdom` pragma for DOM tests.
  - Mock fetch and localStorage with `vi.spyOn()` + `afterEach(vi.clearAllMocks())`.
  - See `src/lib/http-client.test.ts` and `src/lib/store-persist.test.ts` for patterns.
- **Coverage thresholds**: Minimum 5% per metric (lines, functions, statements, branches) via v8 provider.
- **Single-test examples**:
  ```bash
  npm run test -- src/features/trunk/types.test.ts
  npm run test -- -t "isRegisterActionDisabled"
  ```

## Code Style

- **Prettier**: No semicolons, single quotes, trailing commas.
- **ESLint**: TanStack config (`@tanstack/eslint-config`).
- **Format + lint**: `npm run check` = prettier --write + eslint --fix.

## Environment Setup

Before running frontend:

1. Copy/create `.env` file (see repo README for VITE*\* variables: `VITE_GATEWAY_URL`, `VITE_TURN*_`, `VITE*KEYCLOAK*_`).
2. Run `npm install` in `apps/gateway-frontend`.
3. Dev: `npm run dev` (port 3150, Vite server).
4. Build: `npm run build` → `dist/`.

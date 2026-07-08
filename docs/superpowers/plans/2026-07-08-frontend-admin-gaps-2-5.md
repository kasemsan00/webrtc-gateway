# Frontend Admin Gaps (Items 2–5) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close four admin UI gaps vs gateway: session-scoped mobile client diagnostics, live log follow UX, gateway health visibility, and REST hangup/DTMF on active sessions.

**Architecture:** Frontend-only changes in `apps/frontend`: thin API modules using existing `fetchJson` / `resolveGatewayApiBaseUrl`, UI tabs and controls on existing routes (`sessions.$sessionId`, `active-sessions`, `gateway-logs`, `dashboard`). No gateway contract changes. Reuse `SessionEvent` / `SessionPayload` types where gateway responses match existing session-detail shapes.

**Tech Stack:** React 19, TypeScript strict, TanStack Router, Vitest, existing `@/lib/http-client`, gateway REST (`apps/gateway/internal/api/server.go` routes).

## Global Constraints

- Do not edit `apps/frontend/src/routeTree.gen.ts` manually.
- Prettier: no semicolons, single quotes, trailing commas.
- Use `import type` for type-only imports.
- Prefer `@/*` paths; validate API payloads at boundaries; avoid `any`.
- If REST contracts change, update gateway + frontend together (this plan does **not** change gateway).
- Run verification from repo root: `pnpm --filter frontend run test`, `pnpm --filter frontend run check`.
- Never commit secrets.

## Baseline / rollback note

If a prior session added only `apps/frontend/src/features/active-sessions/services/session-control-api.ts` (+ `.test.ts`) without wiring UI, treat that as **partial Task 1** (API layer only). Either keep and add UI in Task 1 Step 3+, or `git restore` those files and redo Task 1 from Step 1 TDD. **Do not** leave orphan API files without Active Sessions UI using them.

## Gateway endpoints (reference)

| Item | Method | Path |
|------|--------|------|
| 2 Client diag events | GET | `/api/client-diagnostics/sessions/{sessionId}/events` |
| 2 Client diag payloads | GET | `/api/client-diagnostics/sessions/{sessionId}/payloads` |
| 2 Client diag body | GET | `/api/client-diagnostics/payloads/{payloadId}` (already used globally) |
| 3 Logs | GET | `/api/logs/current?tail=N`, `/api/logs/{name}?tail=N` (existing) |
| 4 Health | GET | `/api/dashboard` (JWT when `AUTH_ENABLE`) |
| 5 Hangup | POST | `/api/hangup/{sessionId}` |
| 5 DTMF | POST | `/api/dtmf/{sessionId}` body `{ "digits": "..." }` |

---

## File map

| File | Responsibility |
|------|----------------|
| `features/active-sessions/services/session-control-api.ts` | `hangupSession`, `sendSessionDtmf` |
| `features/active-sessions/components/active-sessions-page.tsx` | Actions column, confirm hangup, DTMF dialog |
| `features/client-diagnostics/services/client-diagnostics-api.ts` | Session-scoped list fetchers |
| `features/session-detail/components/session-detail-page.tsx` | Tab `client-diagnostics` + sub-views |
| `features/gateway-logs/components/gateway-logs-page.tsx` | Live follow, interval options, scroll anchor |
| `features/dashboard/services/dashboard-api.ts` | `fetchGatewayHealth` → `/api/dashboard` |
| `features/dashboard/components/dashboard-page.tsx` | Health card (instance, uptime, DB, live counts) |

---

### Task 1: REST session control API (hangup + DTMF)

**Files:**
- Create or verify: `apps/frontend/src/features/active-sessions/services/session-control-api.ts`
- Create or verify: `apps/frontend/src/features/active-sessions/services/session-control-api.test.ts`

**Interfaces:**
- Consumes: `fetchJson`, `resolveGatewayApiBaseUrl` from `@/lib/http-client`
- Produces:
  - `hangupSession(sessionId: string): Promise<{ sessionId: string; state: string; message: string }>`
  - `sendSessionDtmf(sessionId: string, digits: string): Promise<{ sessionId: string; message: string }>`

- [ ] **Step 1: Write failing tests**

```typescript
// apps/frontend/src/features/active-sessions/services/session-control-api.test.ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { hangupSession, sendSessionDtmf } from './session-control-api'

describe('session-control-api', () => {
  afterEach(() => vi.restoreAllMocks())

  it('posts hangup for session id', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: () => ({ sessionId: 'sess-1', state: 'ended', message: 'Call ended' }),
    })
    vi.stubGlobal('fetch', fetchMock)
    const result = await hangupSession('sess-1')
    expect(result.state).toBe('ended')
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain('/hangup/sess-1')
  })

  it('posts dtmf digits', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: () => ({ sessionId: 'sess-2', message: 'DTMF sent' }),
    })
    vi.stubGlobal('fetch', fetchMock)
    await sendSessionDtmf('sess-2', '12#')
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit
    expect(init.method).toBe('POST')
    expect(init.body).toBe(JSON.stringify({ digits: '12#' }))
  })
})
```

- [ ] **Step 2: Run test to verify fail (if implementation missing)**

Run: `pnpm --filter frontend run test -- src/features/active-sessions/services/session-control-api.test.ts`

Expected: FAIL only if `session-control-api.ts` is absent or wrong.

- [ ] **Step 3: Implement API module**

```typescript
// apps/frontend/src/features/active-sessions/services/session-control-api.ts
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'

const API_BASE = resolveGatewayApiBaseUrl()

export interface HangupSessionResponse {
  sessionId: string
  state: string
  message: string
}

export interface SendDtmfResponse {
  sessionId: string
  message: string
}

export async function hangupSession(sessionId: string): Promise<HangupSessionResponse> {
  return fetchJson<HangupSessionResponse>(
    `${API_BASE}/hangup/${encodeURIComponent(sessionId)}`,
    { method: 'POST' },
  )
}

export async function sendSessionDtmf(
  sessionId: string,
  digits: string,
): Promise<SendDtmfResponse> {
  return fetchJson<SendDtmfResponse>(
    `${API_BASE}/dtmf/${encodeURIComponent(sessionId)}`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ digits }),
    },
  )
}
```

- [ ] **Step 4: Run tests**

Run: `pnpm --filter frontend run test -- src/features/active-sessions/services/session-control-api.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/frontend/src/features/active-sessions/services/session-control-api.ts apps/frontend/src/features/active-sessions/services/session-control-api.test.ts
git commit -m "feat(frontend): add REST hangup and DTMF API helpers"
```

---

### Task 2: Active Sessions admin actions UI

**Files:**
- Modify: `apps/frontend/src/features/active-sessions/components/active-sessions-page.tsx`

**Interfaces:**
- Consumes: `hangupSession`, `sendSessionDtmf` from `../services/session-control-api`
- Produces: UI only (no new exports required)

- [ ] **Step 1: Add actions column**

In `active-sessions-page.tsx`:
- Import `Link` from `@tanstack/react-router`, `hangupSession`, `sendSessionDtmf`.
- Add state: `dtmfSessionId: string | null`, `dtmfDigits: string`, `actionError: string | null`, `actionLoadingId: string | null`.
- Add column `id: 'actions'` with:
  - Link to `/sessions/$sessionId` with `params={{ sessionId: row.original.id }}` (label "Detail").
  - Button "Hangup" — `window.confirm` then `hangupSession`, then `load()`.
  - Button "DTMF" — sets `dtmfSessionId` to open dialog.

DTMF validation before submit: `/^[0-9*#]+$/` and length 1–32; show error in dialog if invalid.

- [ ] **Step 2: DTMF dialog**

Use existing `@/components/ui/dialog`, `Input`, `Button`:
- Title: `Send DTMF — {sessionId}`
- On submit: `sendSessionDtmf(dtmfSessionId, dtmfDigits)`, close on success, toast or inline success message optional (inline `actionError` is enough).

- [ ] **Step 3: Manual smoke**

Run: `pnpm dev:frontend` — open Active Sessions with a test gateway session; hangup removes row after refresh/SSE.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/src/features/active-sessions/components/active-sessions-page.tsx
git commit -m "feat(frontend): admin hangup and DTMF on active sessions"
```

---

### Task 3: Session-scoped client diagnostics API

**Files:**
- Modify: `apps/frontend/src/features/client-diagnostics/services/client-diagnostics-api.ts`
- Modify: `apps/frontend/src/features/client-diagnostics/services/client-diagnostics-api.test.ts`

**Interfaces:**
- Consumes: `SessionEventListResponse`, `SessionPayloadListResponse` from `@/features/session-detail/types`
- Produces:
  - `fetchClientDiagnosticSessionEvents(sessionId: string, params?: { page?: number; pageSize?: number; name?: string }): Promise<SessionEventListResponse>`
  - `fetchClientDiagnosticSessionPayloads(sessionId: string, params?: { page?: number; pageSize?: number }): Promise<SessionPayloadListResponse>`
  - Reuse existing `fetchClientDiagnosticPayload(payloadId)` for modal body

- [ ] **Step 1: Write failing test**

```typescript
it('fetches session-scoped client diagnostic events', async () => {
  const fetchMock = vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    headers: new Headers({ 'content-type': 'application/json' }),
    json: () => ({ items: [], total: 0, page: 1, pageSize: 50 }),
  })
  vi.stubGlobal('fetch', fetchMock)
  const { fetchClientDiagnosticSessionEvents } = await import('./client-diagnostics-api')
  await fetchClientDiagnosticSessionEvents('sess-abc', { page: 1, pageSize: 50 })
  expect(String(fetchMock.mock.calls[0]?.[0])).toContain(
    '/client-diagnostics/sessions/sess-abc/events',
  )
})
```

- [ ] **Step 2: Implement fetchers**

```typescript
export async function fetchClientDiagnosticSessionEvents(
  sessionId: string,
  params: { page?: number; pageSize?: number; name?: string } = {},
): Promise<SessionEventListResponse> {
  const url = appendQuery(
    `${API_BASE}/client-diagnostics/sessions/${encodeURIComponent(sessionId)}/events`,
    { page: params.page, pageSize: params.pageSize, name: params.name },
  )
  return fetchJson<SessionEventListResponse>(url)
}

export async function fetchClientDiagnosticSessionPayloads(
  sessionId: string,
  params: { page?: number; pageSize?: number } = {},
): Promise<SessionPayloadListResponse> {
  const url = appendQuery(
    `${API_BASE}/client-diagnostics/sessions/${encodeURIComponent(sessionId)}/payloads`,
    { page: params.page, pageSize: params.pageSize },
  )
  return fetchJson<SessionPayloadListResponse>(url)
}
```

- [ ] **Step 3: Run tests**

Run: `pnpm --filter frontend run test -- src/features/client-diagnostics/services/client-diagnostics-api.test.ts`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/src/features/client-diagnostics/services/client-diagnostics-api.ts apps/frontend/src/features/client-diagnostics/services/client-diagnostics-api.test.ts
git commit -m "feat(frontend): client diagnostics session API helpers"
```

---

### Task 4: Session detail — Client diagnostics tab

**Files:**
- Modify: `apps/frontend/src/features/session-detail/components/session-detail-page.tsx`

**Interfaces:**
- Consumes: Task 3 fetchers + `fetchClientDiagnosticPayload`
- Produces: Tab `client-diagnostics` with Events / Payloads sub-tabs (mirror existing Events/Payloads tab patterns)

- [ ] **Step 1: Extend `TabType`**

Change:
`type TabType = 'events' | 'payloads' | 'dialogs' | 'stats'`
to include `'client-diagnostics'`.

Add tab button label `Client diagnostics`.

- [ ] **Step 2: Implement `ClientDiagnosticsTab`**

New component in same file (or extract to `client-diagnostics-tab.tsx` if file exceeds ~900 lines):
- Sub-toggle: `events` | `payloads`
- Events: table columns same as `EventsTab` (timestamp, name, level in data if present); data from `fetchClientDiagnosticSessionEvents`
- Payloads: list `kind === client_diagnostics_batch`; View opens modal via `fetchClientDiagnosticPayload` (not `fetchPayload` from session payloads API)

- [ ] **Step 3: Empty state copy**

When total === 0: "No mobile client diagnostics for this session."

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/src/features/session-detail/components/session-detail-page.tsx
git commit -m "feat(frontend): client diagnostics tab on session detail"
```

---

### Task 5: Gateway logs live follow UX

**Files:**
- Modify: `apps/frontend/src/features/gateway-logs/components/gateway-logs-page.tsx`

**Interfaces:**
- Consumes: existing `fetchLogTail`, `fetchLogFiles`
- Produces: improved live tail behavior only

- [ ] **Step 1: Interval options**

Replace fixed 5s with `const REFRESH_INTERVALS = [2, 5, 10] as const` and state `refreshIntervalSec` default `5`.

When `autoRefresh` enabled, `setInterval(..., refreshIntervalSec * 1000)`.

Header: toggle "Live" (rename from "Auto 5s") + small select for 2/5/10s.

- [ ] **Step 2: Stick to bottom on live refresh**

Add `logEndRef` on a div after `<pre>`; `const stickToBottomRef = useRef(true)`.

On scroll container: if `scrollHeight - scrollTop - clientHeight < 48` then `stickToBottomRef.current = true`, else false.

After `setLines` in `loadTail`, if `autoRefresh && stickToBottomRef.current`, `requestAnimationFrame(() => logEndRef.current?.scrollIntoView())`.

- [ ] **Step 3: Default live on current file**

When user selects file with `file.current === true`, optionally set `autoRefresh` true once (use ref `didAutoEnableLive` to avoid fighting user who turned it off).

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/src/features/gateway-logs/components/gateway-logs-page.tsx
git commit -m "feat(frontend): live log follow with configurable interval"
```

---

### Task 6: Dashboard gateway health card

**Files:**
- Modify: `apps/frontend/src/features/dashboard/services/dashboard-api.ts`
- Modify: `apps/frontend/src/features/dashboard/types.ts` (if `GatewayDashboard` not re-exported)
- Modify: `apps/frontend/src/features/dashboard/components/dashboard-page.tsx`

**Interfaces:**
- Consumes: `GatewayDashboard` type from `@/features/gateway-instances/types` (same shape as `GET /api/dashboard`)
- Produces: `fetchGatewayHealth(): Promise<GatewayDashboard>`

- [ ] **Step 1: Add API function**

```typescript
import type { GatewayDashboard } from '@/features/gateway-instances/types'

export async function fetchGatewayHealth(): Promise<GatewayDashboard> {
  return fetchJson<GatewayDashboard>(`${API_BASE}/dashboard`)
}
```

- [ ] **Step 2: Dashboard UI block**

Near top of `DashboardPage` (below period controls or in metrics grid):
- `useEffect` poll `fetchGatewayHealth` every 30s when tab visible (same pattern as instances page).
- Card showing: `instanceId`, `uptimeSeconds` (formatted `Xh Ym Zs`), `dbConnected` badge, `activeSessions`, `wsClients`, `registeredTrunks` / `enabledTrunks`.
- Link: "Instances" → `/instances`.

- [ ] **Step 3: Error state**

Non-blocking banner if health fetch fails; summary charts still load.

- [ ] **Step 4: Commit**

```bash
git add apps/frontend/src/features/dashboard/services/dashboard-api.ts apps/frontend/src/features/dashboard/components/dashboard-page.tsx
git commit -m "feat(frontend): gateway health card on dashboard"
```

---

### Task 7: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Run frontend tests**

```bash
pnpm --filter frontend run test
```

Expected: all PASS

- [ ] **Step 2: Lint and format**

```bash
pnpm --filter frontend run check
```

Expected: PASS

- [ ] **Step 3: Update frontend AGENTS.md (optional one-liner)**

Under API notes, mention session detail client diagnostics tab and active session REST hangup — only if you want docs parity.

- [ ] **Step 4: Commit (if docs touched)**

```bash
git add apps/frontend/AGENTS.md
git commit -m "docs(frontend): note admin session control and client diagnostics"
```

---

## Spec coverage (self-review)

| Requirement | Task |
|-------------|------|
| 2 Client diagnostics per session | 3, 4 |
| 3 Live logs | 5 |
| 4 Health / config visibility (`/api/dashboard`, not raw env) | 6 |
| 5 Admin hangup/DTMF on active sessions | 1, 2 |

**Out of scope (YAGNI):** `GET /api/session/{id}` dedicated UI (list row already has fields); exposing full gateway env in UI; SSE for logs; gateway code changes.

**Placeholder scan:** No TBD steps; each task has concrete paths and commands.

**Type consistency:** `GatewayDashboard` shared with gateway-instances; session diag events reuse `SessionEventListResponse`.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-08-frontend-admin-gaps-2-5.md`. Two execution options:

**1. Subagent-Driven (recommended)** — fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
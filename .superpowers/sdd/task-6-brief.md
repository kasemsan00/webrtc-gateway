### Task 6: Frontend — WS clients page component (single SSE subscription)

**Files:**
- Create: `apps/frontend/src/features/ws-clients/components/ws-clients-page.tsx`

**Interfaces:**
- Consumes: `fetchWSClients`, `subscribeWSClientEvents` from `../services/ws-clients-api`; `hangupSession`, `sendSessionDtmf` from `@/features/active-sessions/services/session-control-api`
- Produces: `WSClientsPage` React component

**Pre-flight resolution:** The plan originally had `useVisibilityRealtimeReload` + a separate `useEffect` for SSE (double subscription). The resolution is: **remove `useVisibilityRealtimeReload` entirely**, use only a single `useEffect` for SSE subscription. SSE persists across visibility changes; on SSE error, fallback to `loadRef.current()` (poll).

- [ ] **Step 1: Implement the page**

Read the full page component code from the plan file:
`E:\dev\webrtc-gateway\docs\superpowers\plans\2026-07-08-ws-clients-realtime.md` starting at line 800 (### Task 6).

The complete code is in the plan from line 811 to ~1130. Copy it verbatim into `apps/frontend/src/features/ws-clients/components/ws-clients-page.tsx`.

Key features:
- SSE subscription via single `useEffect` (no `useVisibilityRealtimeReload`)
- Table with columns: Client ID, Session, Trunk (badge), Availability (badge), Call State, Auth, Actions
- Actions (Hangup/DTMF) only shown for active rows (`ACTIVE_CALL_STATES.has(callState)`)
- DTMF dialog with validation (`/^[0-9*#]+$/`, max 32 chars)
- Error/actionError banners
- Refresh button + theme toggle

- [ ] **Step 2: Verify build**

Run: `pnpm --filter frontend run test`

Expected: all existing tests pass (no new test required for this task — page is a UI component, tested via smoke test in Task 7)

- [ ] **Step 3: Commit**

```bash
cd E:\dev\webrtc-gateway
git add apps/frontend/src/features/ws-clients/components/ws-clients-page.tsx
git commit -m "feat(frontend): add ws-clients real-time page component"
```

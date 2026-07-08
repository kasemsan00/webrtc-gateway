### Task 7: Frontend — route + menu entry

**Files:**
- Create: `apps/frontend/src/routes/ws-clients.tsx`
- Modify: `apps/frontend/src/components/Header.tsx` (add menu link between Active Sessions and Gateway Logs)
- Regenerate: `apps/frontend/src/routeTree.gen.ts` (auto — run dev server briefly)

- [ ] **Step 1: Create route file**

```tsx
// apps/frontend/src/routes/ws-clients.tsx
import { createFileRoute } from '@tanstack/react-router'
import { WSClientsPage } from '@/features/ws-clients/components/ws-clients-page'

export const Route = createFileRoute('/ws-clients')({
  component: WSClientsPage,
})
```

- [ ] **Step 2: Add menu entry in Header**

In `apps/frontend/src/components/Header.tsx`, find the Active Sessions link (around line 220). After it, add a new link for WS Clients:

```tsx
                <Link
                  to="/ws-clients"
                  onClick={close}
                  className="flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm transition-colors hover:bg-muted"
                  activeProps={{
                    className:
                      'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm bg-cyan-600/10 text-cyan-700 dark:bg-cyan-600/20 dark:text-cyan-300 hover:bg-cyan-600/20 dark:hover:bg-cyan-600/30 transition-colors',
                  }}
                >
                  <RiRouterLine size={16} />
                  <span className="font-medium">WS Clients</span>
                </Link>
```

Add `RiRouterLine` to the remixicon import block at the top of the file (after `RiRouteLine` or in alphabetical order within the import).

- [ ] **Step 3: Regenerate route tree**

Run: `pnpm dev:frontend` — start the dev server, wait a few seconds for route tree to regenerate (TanStack Start auto-generates `routeTree.gen.ts` on startup), then stop it (Ctrl+C).

Verify: `apps/frontend/src/routeTree.gen.ts` contains `/ws-clients` route entry.

- [ ] **Step 4: Run frontend tests + check**

Run: `pnpm --filter frontend run test && pnpm --filter frontend run check`

Expected: tests PASS; check may have pre-existing eslint errors in unrelated files (rtt-xep0301.ts) but no NEW errors in ws-clients files.

- [ ] **Step 5: Commit**

```bash
cd E:\dev\webrtc-gateway
git add apps/frontend/src/routes/ws-clients.tsx apps/frontend/src/components/Header.tsx apps/frontend/src/routeTree.gen.ts
git commit -m "feat(frontend): add /ws-clients route and menu entry"
```

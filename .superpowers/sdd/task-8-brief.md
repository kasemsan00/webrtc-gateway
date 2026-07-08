### Task 8: Final verification + docs

**Files:**
- Modify: `apps/gateway/AGENTS.md` (section 5 file map — update handlers_sse.go row)
- Modify: `docs/gateway/ops-guide.md` (add /ws-clients/stream endpoint)

- [ ] **Step 1: Run gateway tests**

```bash
cd apps/gateway && go test ./...
```

Expected: PASS

- [ ] **Step 2: Run frontend tests**

```bash
pnpm --filter frontend run test
```

Expected: PASS

- [ ] **Step 3: Update AGENTS.md**

In `apps/gateway/AGENTS.md`, section 5 (internal/api/ File Map), update the `handlers_sse.go` row:

Change:
```
| `handlers_sse.go` | trunk/session SSE streams |
```
To:
```
| `handlers_sse.go` | trunk/session/ws-client SSE streams |
```

- [ ] **Step 4: Update ops-guide.md**

In `docs/gateway/ops-guide.md`, add after the gateway process logs section:

```
WebSocket clients real-time stream:

- `GET /api/ws-clients/stream` — SSE stream of WS client connect/disconnect/update events (each event carries the full `WSClientResponse`).
```

- [ ] **Step 5: Commit**

```bash
cd E:\dev\webrtc-gateway
git add apps/gateway/AGENTS.md docs/gateway/ops-guide.md
git commit -m "docs: document /ws-clients/stream SSE endpoint"
```

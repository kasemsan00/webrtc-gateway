### Task 4: Gateway — hook notifyWSClientChanged at all state-change points (+ add RLock to notifyWSClientChanged)

**Files:**
- Modify: `apps/gateway/internal/api/handlers_sse.go` (add RLock to notifyWSClientChanged — concurrency fix from Task 3 review)
- Modify: `apps/gateway/internal/api/ws_conn.go:104-106` (after wsConnections register) + `:142-149` (disconnect)
- Modify: `apps/gateway/internal/api/ws_trunk.go:232-233` (after trunk resolve)
- Modify: `apps/gateway/internal/api/ws_midcall.go:132-135` (after client_state)
- Modify: `apps/gateway/internal/api/ws_call.go:63-65` (session register) + `:310-311` (trunk reset)
- Modify: `apps/gateway/internal/api/ws_resume.go:215` (register)
- Modify: `apps/gateway/internal/api/ws_incoming.go:518` (register)

**Interfaces:**
- Consumes: `s.notifyWSClientChanged` from Task 3
- Produces: SSE events emitted on every state change

**CRITICAL CONCURRENCY FIX (from Task 3 review):**

`notifyWSClientChanged` (in `handlers_sse.go`) currently calls `buildWSClientResponse` without holding `s.mu.RLock()`. The client fields (`sessionID`, `callState`, `availability`, `trunkResolved`, `resolvedTrunkID`) are mutated under `s.mu.Lock()` by WS handlers. Reading them without the lock is a data race.

**Fix:** In `notifyWSClientChanged`, acquire `s.mu.RLock()` for the `buildWSClientResponse` call, then release before `broadcastWSClientStream` (which acquires its own `s.mu.RLock` — Go RWMutex does not support recursive RLock when a writer is waiting).

Updated `notifyWSClientChanged`:

```go
func (s *Server) notifyWSClientChanged(eventType string, client *WSClient) {
	if client == nil {
		return
	}
	s.mu.RLock()
	resp := s.buildWSClientResponse(client)
	s.mu.RUnlock()
	payload, err := json.Marshal(WSClientStreamEvent{
		Type:     eventType,
		ClientID: client.clientID,
		At:       time.Now().Format(time.RFC3339Nano),
		Client:   &resp,
	})
	if err != nil {
		return
	}
	s.broadcastWSClientStream(payload)
}
```

Note: `buildWSClientResponse` calls `s.trunkManager.GetTrunkByID` which acquires `tm.mu` (disjoint from `s.mu`), so no deadlock. The `clientID` read after RUnlock is safe because `clientID` is set once at connect and never mutated.

- [ ] **Step 0: Apply the concurrency fix to notifyWSClientChanged**

In `apps/gateway/internal/api/handlers_sse.go`, update `notifyWSClientChanged` to add `s.mu.RLock()` / `s.mu.RUnlock()` around the `buildWSClientResponse` call as shown above.

- [ ] **Step 1: ws_conn.go — notify on connect + disconnect**

In `apps/gateway/internal/api/ws_conn.go`, after the `wsConnections` register block (line 106):

```go
	s.mu.Lock()
	s.wsConnections[client] = struct{}{}
	s.mu.Unlock()
	s.notifyWSClientChanged("connected", client)
```

On disconnect (line 142-149), capture the client before delete and notify after unlock:

```go
	s.mu.Lock()
	delete(s.wsConnections, client)
	if client.sessionID != "" {
		if s.wsClients[client.sessionID] == client {
			delete(s.wsClients, client.sessionID)
		}
	}
	s.mu.Unlock()
	s.notifyWSClientChanged("disconnected", client)
```

- [ ] **Step 2: ws_trunk.go — notify after trunk resolve**

In `apps/gateway/internal/api/ws_trunk.go`, after `client.resolvedTrunkID = trunkID` (line 233), before the authClaims block:

```go
		client.trunkResolved = true
		client.resolvedTrunkID = trunkID
		s.notifyWSClientChanged("updated", client)
```

Note: this is called without holding `s.mu` — the notifyWSClientChanged function now acquires `s.mu.RLock()` internally, which is safe.

- [ ] **Step 3: ws_midcall.go — notify after client_state**

In `apps/gateway/internal/api/ws_midcall.go`, after the unlock (line 135):

```go
	s.mu.Lock()
	client.availability = availability
	client.callState = callState
	s.mu.Unlock()
	s.notifyWSClientChanged("updated", client)
```

- [ ] **Step 4: ws_call.go — notify on session register + trunk reset**

In `apps/gateway/internal/api/ws_call.go`, after session register (line 65):

```go
	client.sessionID = sess.ID
	s.mu.Lock()
	s.wsClients[sess.ID] = client
	s.mu.Unlock()
	s.notifyWSClientChanged("updated", client)
```

After trunk reset (line 311):

```go
		client.trunkResolved = false
		client.resolvedTrunkID = 0
		s.notifyWSClientChanged("updated", client)
```

Note: trunk reset at line 310-311 is inside a code path where `s.mu` may or may not be held — check the surrounding context. If `s.mu` is held, the notify must be moved after the unlock (notifyWSClientChanged acquires its own RLock which would deadlock if caller holds Lock). Read the function context around line 300-315 to determine lock state.

- [ ] **Step 5: ws_resume.go — notify on register**

In `apps/gateway/internal/api/ws_resume.go`, after `s.wsClients[msg.SessionID] = client` (line 215):

```go
	s.wsClients[msg.SessionID] = client
	s.notifyWSClientChanged("updated", client)
```

Check if `s.mu` is held at this point — if yes, move the notify after unlock.

- [ ] **Step 6: ws_incoming.go — notify on register**

In `apps/gateway/internal/api/ws_incoming.go`, after `s.wsClients[callSession.ID] = client` (line 518):

```go
	s.wsClients[callSession.ID] = client
	s.notifyWSClientChanged("updated", client)
```

Check if `s.mu` is held at this point — if yes, move the notify after unlock.

- [ ] **Step 7: Run full gateway suite**

Run: `cd apps/gateway && go test ./...`

Expected: PASS. If tests fail due to lock issues (calling RLock while holding Lock), move the `notifyWSClientChanged` call to after any `s.mu.Unlock()` in that code path.

- [ ] **Step 8: Run with race detector**

Run: `cd apps/gateway && go test -race ./internal/api/...`

Expected: PASS (no data races detected)

- [ ] **Step 9: Commit**

```bash
cd E:\dev\webrtc-gateway
git add apps/gateway/internal/api/handlers_sse.go apps/gateway/internal/api/ws_conn.go apps/gateway/internal/api/ws_trunk.go apps/gateway/internal/api/ws_midcall.go apps/gateway/internal/api/ws_call.go apps/gateway/internal/api/ws_resume.go apps/gateway/internal/api/ws_incoming.go
git commit -m "feat(gateway): emit ws-client SSE events on state changes (+ RLock race fix)"
```

### Task 3: Gateway — notifyWSClientChanged + handleWSClientsStream + route + DRY refactor

**Files:**
- Modify: `apps/gateway/internal/api/handlers_sse.go` (add event type, notify fn, handler, buildWSClientResponse helper)
- Modify: `apps/gateway/internal/api/handlers_ops.go` (DRY refactor: handleListWSClients now uses buildWSClientResponse)
- Modify: `apps/gateway/internal/api/server.go:270` (register route)
- Modify: `apps/gateway/internal/api/handlers_api_test.go` (add stream test)

**Interfaces:**
- Consumes: `s.broadcastWSClientStream` from Task 2
- Produces: `s.notifyWSClientChanged(eventType string, client *WSClient)` — called by state-change hooks in Task 4

**Pre-flight resolution (DRY):** `buildWSClientResponse` (created in this task) replaces the inline per-client logic in `handleListWSClients` (from Task 1). After creating `buildWSClientResponse`, refactor `handleListWSClients` to use it.

- [ ] **Step 1: Write failing test for handleWSClientsStream**

Add to `apps/gateway/internal/api/handlers_api_test.go`:

```go
func TestHandleWSClientsStream_Contract(t *testing.T) {
	srv, _ := newAPIHandlerTestServer(t, nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/ws-clients/stream", nil).WithContext(ctx)
	writer := &flushRecorder{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleWSClientsStream(writer, req)
	}()

	deadline := time.Now().Add(750 * time.Millisecond)
	for {
		writer.mu.Lock()
		wroteConnected := strings.Contains(writer.body.String(), "event: connected")
		writer.mu.Unlock()
		if wroteConnected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ws-client stream did not write connected event in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("ws-client stream handler did not stop after context cancellation")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/gateway && go test ./internal/api -run TestHandleWSClientsStream_Contract -v`

Expected: FAIL — `handleWSClientsStream` not defined.

- [ ] **Step 3: Add WSClientStreamEvent + notifyWSClientChanged + buildWSClientResponse + handleWSClientsStream**

In `apps/gateway/internal/api/handlers_sse.go`, add after `SessionStreamEvent`:

```go
type WSClientStreamEvent struct {
	Type      string             `json:"type"`
	ClientID  string             `json:"clientId"`
	At        string             `json:"at"`
	Client    *WSClientResponse  `json:"client,omitempty"`
}
```

Add after `notifySessionListChanged`:

```go
func (s *Server) notifyWSClientChanged(eventType string, client *WSClient) {
	if client == nil {
		return
	}
	resp := s.buildWSClientResponse(client)
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

Add `buildWSClientResponse` helper in `handlers_sse.go`:

```go
func (s *Server) buildWSClientResponse(client *WSClient) WSClientResponse {
	resp := WSClientResponse{
		ClientID:        client.clientID,
		ConnectedAt:     client.ConnectedAt.Format(time.RFC3339),
		TrunkResolved:   client.trunkResolved,
		ResolvedTrunkID: client.resolvedTrunkID,
		Availability:    client.availability,
		CallState:       client.callState,
		PublicOnly:      client.publicOnly,
	}
	if client.sessionID != "" {
		resp.SessionID = client.sessionID
	}
	if client.authClaims != nil {
		resp.AuthSubject = client.authClaims.Subject
	}
	if s.trunkManager != nil && client.resolvedTrunkID > 0 {
		if trunkRaw, ok := s.trunkManager.GetTrunkByID(client.resolvedTrunkID); ok {
			if trunk, ok := trunkRaw.(*sip.Trunk); ok && trunk.PublicID != "" {
				resp.ResolvedTrunkPublicID = trunk.PublicID
			}
		}
	}
	return resp
}
```

Add the handler (mirror `handleSessionStream`, event name `"ws-client"`):

```go
func (s *Server) handleWSClientsStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.respondError(w, http.StatusInternalServerError, "Streaming unsupported")
		return
	}

	id, ch := s.subscribeWSClientStream()
	defer s.unsubscribeWSClientStream(id)

	connectedPayload, _ := json.Marshal(WSClientStreamEvent{
		Type: "connected",
		At:   time.Now().Format(time.RFC3339Nano),
	})
	if err := s.writeSSE(w, "connected", connectedPayload); err != nil {
		return
	}

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-ch:
			if err := s.writeSSE(w, "ws-client", payload); err != nil {
				return
			}
		case <-heartbeat.C:
			heartbeatPayload, _ := json.Marshal(WSClientStreamEvent{
				Type: "heartbeat",
				At:   time.Now().Format(time.RFC3339Nano),
			})
			if err := s.writeSSE(w, "heartbeat", heartbeatPayload); err != nil {
				return
			}
		}
	}
}
```

You need to add `"k2-gateway/internal/sip"` to the imports in `handlers_sse.go` for the `*sip.Trunk` type assertion in `buildWSClientResponse`.

- [ ] **Step 4: DRY refactor — handleListWSClients uses buildWSClientResponse**

In `apps/gateway/internal/api/handlers_ops.go`, replace the body of `handleListWSClients` to use the new helper:

```go
// handleListWSClients returns all connected WebSocket clients (including idle)
func (s *Server) handleListWSClients(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	clients := make([]WSClientResponse, 0, len(s.wsConnections))
	for client := range s.wsConnections {
		clients = append(clients, s.buildWSClientResponse(client))
	}
	s.mu.RUnlock()

	s.respondJSON(w, http.StatusOK, clients)
}
```

Note: `buildWSClientResponse` calls `s.trunkManager.GetTrunkByID` which is a read-only cache lookup (sync.Map-based), safe under RLock. The trunk public-ID enrichment that was previously done in a separate loop after RUnlock is now inside `buildWSClientResponse`. If `GetTrunkByID` acquires any lock that conflicts with `s.mu`, move the `buildWSClientResponse` calls outside the RLock loop (collect clients under lock, then build responses after unlock). Check `trunk_manager.go` `GetTrunkByID` implementation — if it uses `sync.RMap` or its own mutex (not `s.mu`), it's safe under `s.mu.RLock`.

- [ ] **Step 5: Register route**

In `apps/gateway/internal/api/server.go`, after `api.HandleFunc("/ws-clients", ...)` (line 270):

```go
		api.HandleFunc("/ws-clients/stream", s.handleWSClientsStream).Methods("GET", "OPTIONS")
```

- [ ] **Step 6: Run tests**

Run: `cd apps/gateway && go test ./internal/api -run "TestHandleWSClientsStream_Contract|TestHandleSSEStreams|TestHandleListWSClients" -v`

Expected: PASS

- [ ] **Step 7: Run full suite**

Run: `cd apps/gateway && go test ./...`

Expected: PASS

- [ ] **Step 8: Commit**

```bash
cd E:\dev\webrtc-gateway
git add apps/gateway/internal/api/handlers_sse.go apps/gateway/internal/api/handlers_ops.go apps/gateway/internal/api/server.go apps/gateway/internal/api/handlers_api_test.go
git commit -m "feat(gateway): add /ws-clients/stream SSE endpoint + DRY refactor handleListWSClients"
```

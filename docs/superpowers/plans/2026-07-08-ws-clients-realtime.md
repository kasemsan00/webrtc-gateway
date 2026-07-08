# WS Clients Real-Time View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a real-time admin page showing all connected WebSocket clients with their resolved trunk, availability, call state, and auth subject, plus admin hangup/DTMF on active rows.

**Architecture:** Gateway: add `clientID` (UUID) to `WSClient`, change `handleListWSClients` to iterate `wsConnections` (all connected WS, not just session-active), add SSE `/api/ws-clients/stream` mirroring `sessions/stream`/`trunks/stream`, and emit events on every WSClient state change. Frontend: new `/ws-clients` page subscribing to the SSE stream, upserting/removing rows by `clientID`, reusing `hangupSession`/`sendSessionDtmf` from `active-sessions/services/session-control-api`.

**Tech Stack:** Go 1.26.2, gorilla/mux, `github.com/google/uuid` v1.6.0 (already in go.mod), React 19, TypeScript strict, TanStack Router, Vitest.

## Global Constraints

- No panics in hot paths; SSE/WS loops log and continue.
- Preserve wire compatibility: `WSClientResponse` changes are additive (new `clientID`, `sessionId` becomes optional, new `publicOnly`) — existing Instances page must still work (`client.sessionId || '-'` already handles empty).
- WS message changes: update `docs/gateway/ws-contract.md` if a new WS message type is added (this plan adds no new WS message types — only REST/SSE).
- Run `go test ./...` after gateway changes; `pnpm --filter frontend run test` + `pnpm --filter frontend run check` after frontend changes.
- Prettier: no semicolons, single quotes, trailing commas.
- Use `import type` for type-only imports; prefer `@/*` alias.
- Never edit `apps/frontend/src/routeTree.gen.ts` manually — run `pnpm dev:frontend` briefly to regenerate, or it auto-regenerates on dev server start.

---

## File map

| File | Responsibility |
|------|----------------|
| `apps/gateway/internal/api/server.go` | `clientID` field on `WSClient`, `ClientID`/`SessionID`/`PublicOnly` on `WSClientResponse`, `wsClientStreams`/`wsClientStreamSeq` fields + init, route registration |
| `apps/gateway/internal/api/handlers_ops.go` | rewrite `handleListWSClients` to iterate `wsConnections` |
| `apps/gateway/internal/api/handlers_sse.go` | `WSClientStreamEvent`, `notifyWSClientChanged`, `handleWSClientsStream` |
| `apps/gateway/internal/api/ws_util.go` | `subscribeWSClientStream`, `unsubscribeWSClientStream`, `broadcastWSClientStream` |
| `apps/gateway/internal/api/ws_conn.go` | assign `clientID` at connect, notify on connect + disconnect |
| `apps/gateway/internal/api/ws_trunk.go` | notify after trunk resolve |
| `apps/gateway/internal/api/ws_midcall.go` | notify after client_state change |
| `apps/gateway/internal/api/ws_call.go` | notify on session register + trunk reset |
| `apps/gateway/internal/api/ws_resume.go` | notify on register |
| `apps/gateway/internal/api/ws_incoming.go` | notify on register |
| `apps/gateway/internal/api/handlers_api_test.go` | update existing ws-clients test + add SSE stream test |
| `apps/frontend/src/features/ws-clients/types.ts` | `WSClient` type |
| `apps/frontend/src/features/ws-clients/services/ws-clients-api.ts` | `fetchWSClients`, `subscribeWSClientEvents` |
| `apps/frontend/src/features/ws-clients/components/ws-clients-page.tsx` | real-time table + trunk badge + actions |
| `apps/frontend/src/routes/ws-clients.tsx` | route |
| `apps/frontend/src/components/Header.tsx` | menu entry |

---

### Task 1: Gateway — add clientID to WSClient + update WSClientResponse + change handleListWSClients

**Files:**
- Modify: `apps/gateway/internal/api/server.go:100-112` (WSClient struct), `apps/gateway/internal/api/server.go:103-113` (WSClientResponse struct)
- Modify: `apps/gateway/internal/api/handlers_ops.go:259-293` (handleListWSClients)
- Modify: `apps/gateway/internal/api/ws_conn.go:89-106` (assign clientID at connect)
- Modify: `apps/gateway/internal/api/handlers_api_test.go:1206-1240` (update existing test)

**Interfaces:**
- Consumes: `github.com/google/uuid`
- Produces: `WSClient.clientID string`; `WSClientResponse.ClientID string`, `WSClientResponse.SessionID string` (now optional), `WSClientResponse.PublicOnly bool`

- [ ] **Step 1: Write failing test for handleListWSClients iterating wsConnections**

Add to `apps/gateway/internal/api/handlers_api_test.go` after the existing `TestHandleListWSClients_IncludesResolvedTrunkAndClientState`:

```go
func TestHandleListWSClients_IncludesIdleAndClientID(t *testing.T) {
	srv, _ := newAPIHandlerTestServer(t, nil, nil, nil)

	idle := &WSClient{
		clientID:      "idle-uuid-1",
		ConnectedAt:   time.Now(),
		availability:  clientAvailabilityIdle,
		callState:     string(session.StateNew),
		trunkResolved: true,
		resolvedTrunkID: 7,
	}
	srv.wsConnections[idle] = struct{}{}

	active := &WSClient{
		clientID:    "active-uuid-2",
		sessionID:   "sess-active",
		ConnectedAt: time.Now(),
		availability: clientAvailabilityBusy,
		callState:   "incall",
	}
	srv.wsConnections[active] = struct{}{}
	srv.wsClients["sess-active"] = active

	rr := doRequest(t, srv.handleListWSClients, http.MethodGet, "/ws-clients", "", nil)
	resp := assertJSONDecode[[]WSClientResponse](t, rr, http.StatusOK)

	if len(resp) != 2 {
		t.Fatalf("expected 2 ws clients (idle + active), got %d", len(resp))
	}

	var idleResp, activeResp *WSClientResponse
	for i := range resp {
		if resp[i].ClientID == "idle-uuid-1" {
			idleResp = &resp[i]
		}
		if resp[i].ClientID == "active-uuid-2" {
			activeResp = &resp[i]
		}
	}
	if idleResp == nil || !idleResp.TrunkResolved || idleResp.SessionID != "" {
		t.Fatalf("expected idle client with empty sessionID and trunkResolved, got %+v", idleResp)
	}
	if activeResp == nil || activeResp.SessionID != "sess-active" {
		t.Fatalf("expected active client with sess-active, got %+v", activeResp)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/gateway && go test ./internal/api -run TestHandleListWSClients_IncludesIdleAndClientID -v`

Expected: FAIL — `clientID` field unknown, `WSClientResponse` has no `ClientID`, `handleListWSClients` iterates `wsClients` not `wsConnections`.

- [ ] **Step 3: Add clientID to WSClient**

In `apps/gateway/internal/api/server.go`, add `clientID` field and import:

```go
import (
	// existing imports...
	"github.com/google/uuid"
)
```

Add to `WSClient` struct (after `sessionID`):

```go
// WSClient represents a WebSocket client connection
type WSClient struct {
	conn            *websocket.Conn
	clientID        string // UUID assigned at connect, stable across session assignment
	sessionID       string
	trunkResolved   bool
	resolvedTrunkID int64
	availability    string
	callState       string
	send            chan []byte
	ConnectedAt     time.Time
	authClaims      *auth.VerifiedClaims // populated when tokenVerifier is set
	publicOnly      bool                 // true for unauthenticated /ws-public clients
}
```

- [ ] **Step 4: Add ClientID/SessionID/PublicOnly to WSClientResponse**

In `apps/gateway/internal/api/server.go`, update `WSClientResponse`:

```go
// WSClientResponse represents a connected WebSocket client
type WSClientResponse struct {
	ClientID              string `json:"clientId"`
	SessionID             string `json:"sessionId,omitempty"`
	ConnectedAt           string `json:"connectedAt"`
	TrunkResolved         bool   `json:"trunkResolved"`
	ResolvedTrunkID       int64  `json:"resolvedTrunkId,omitempty"`
	ResolvedTrunkPublicID string `json:"resolvedTrunkPublicId,omitempty"`
	Availability          string `json:"availability,omitempty"`
	CallState             string `json:"callState,omitempty"`
	AuthSubject           string `json:"authSubject,omitempty"`
	PublicOnly            bool   `json:"publicOnly,omitempty"`
}
```

- [ ] **Step 5: Rewrite handleListWSClients to iterate wsConnections**

In `apps/gateway/internal/api/handlers_ops.go`, replace `handleListWSClients` (lines 259-293):

```go
// handleListWSClients returns all connected WebSocket clients (including idle)
func (s *Server) handleListWSClients(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	clients := make([]WSClientResponse, 0, len(s.wsConnections))
	for client := range s.wsConnections {
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
		clients = append(clients, resp)
	}
	s.mu.RUnlock()

	if s.trunkManager != nil {
		for idx := range clients {
			if clients[idx].ResolvedTrunkID <= 0 {
				continue
			}
			if trunkRaw, ok := s.trunkManager.GetTrunkByID(clients[idx].ResolvedTrunkID); ok {
				if trunk, ok := trunkRaw.(*sip.Trunk); ok && trunk.PublicID != "" {
					clients[idx].ResolvedTrunkPublicID = trunk.PublicID
				}
			}
		}
	}

	s.respondJSON(w, http.StatusOK, clients)
}
```

- [ ] **Step 6: Assign clientID at connect**

In `apps/gateway/internal/api/ws_conn.go`, add `clientID` to the `WSClient` literal (around line 89):

```go
	client := &WSClient{
		conn:         conn,
		clientID:     uuid.NewString(),
		send:         make(chan []byte, 256),
		availability: clientAvailabilityIdle,
		callState:    string(session.StateNew),
		ConnectedAt:  time.Now(),
		publicOnly:   publicOnly,
	}
```

Add import `"github.com/google/uuid"` at top of `ws_conn.go`.

- [ ] **Step 7: Update existing test to match new response shape**

In `apps/gateway/internal/api/handlers_api_test.go`, update `TestHandleListWSClients_IncludesResolvedTrunkAndClientState` (line ~1206) — add `clientID: "ws-1"` to the `WSClient` literal and register in `wsConnections` instead of (or in addition to) `wsClients`:

```go
func TestHandleListWSClients_IncludesResolvedTrunkAndClientState(t *testing.T) {
	srv, _ := newAPIHandlerTestServer(t, nil, nil, nil)
	now := time.Now()
	client := &WSClient{
		clientID:        "ws-1",
		ConnectedAt:     now,
		trunkResolved:   true,
		resolvedTrunkID: 42,
		availability:    "busy",
		callState:       "incall",
	}
	srv.wsConnections[client] = struct{}{}

	rr := doRequest(t, srv.handleListWSClients, http.MethodGet, "/ws-clients", "", nil)
	resp := assertJSONDecode[[]WSClientResponse](t, rr, http.StatusOK)
	if len(resp) != 1 {
		t.Fatalf("expected 1 client, got %d", len(resp))
	}
	if resp[0].ClientID != "ws-1" || !resp[0].TrunkResolved || resp[0].ResolvedTrunkID != 42 {
		t.Fatalf("unexpected client: %+v", resp[0])
	}
}
```

- [ ] **Step 8: Run tests**

Run: `cd apps/gateway && go test ./internal/api -run "TestHandleListWSClients" -v`

Expected: PASS

- [ ] **Step 9: Run full gateway suite to check regressions**

Run: `cd apps/gateway && go test ./...`

Expected: PASS (existing tests that set `srv.wsClients["session-1"] = &WSClient{...}` will need `wsConnections` too — if any fail, add `srv.wsConnections[client] = struct{}{}` alongside `wsClients` assignments in tests that call `handleListWSClients`. Most tests that use `wsClients` don't call `handleListWSClients` directly.)

- [ ] **Step 10: Commit**

```bash
cd E:\dev\webrtc-gateway
git add apps/gateway/internal/api/server.go apps/gateway/internal/api/handlers_ops.go apps/gateway/internal/api/ws_conn.go apps/gateway/internal/api/handlers_api_test.go
git commit -m "feat(gateway): add clientID to WSClient and show all connections in /ws-clients"
```

---

### Task 2: Gateway — SSE subscribe/broadcast helpers for ws-client stream

**Files:**
- Modify: `apps/gateway/internal/api/server.go:40-50` (fields), `apps/gateway/internal/api/server.go:185-190` (NewServer init)
- Modify: `apps/gateway/internal/api/ws_util.go:224` (append helpers)
- Test: `apps/gateway/internal/api/handlers_api_test.go`

**Interfaces:**
- Consumes: nothing new
- Produces: `s.subscribeWSClientStream() (int, chan []byte)`, `s.unsubscribeWSClientStream(id int)`, `s.broadcastWSClientStream(payload []byte)`

- [ ] **Step 1: Add stream fields to Server**

In `apps/gateway/internal/api/server.go`, after `sessionStreamSeq` (line 45):

```go
	wsClientStreams   map[int]chan []byte
	wsClientStreamSeq int
```

- [ ] **Step 2: Init in NewServer**

In `apps/gateway/internal/api/server.go`, in `NewServer` (after `sessionStreams: make(...)`):

```go
		wsClientStreams:   make(map[int]chan []byte),
```

- [ ] **Step 3: Add subscribe/broadcast helpers**

Append to `apps/gateway/internal/api/ws_util.go` (after `broadcastSessionStream`):

```go
func (s *Server) subscribeWSClientStream() (int, chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.wsClientStreamSeq++
	id := s.wsClientStreamSeq
	ch := make(chan []byte, 32)
	s.wsClientStreams[id] = ch
	return id, ch
}

func (s *Server) unsubscribeWSClientStream(id int) {
	s.mu.Lock()
	delete(s.wsClientStreams, id)
	s.mu.Unlock()
}

func (s *Server) broadcastWSClientStream(payload []byte) {
	s.mu.RLock()
	streams := make([]chan []byte, 0, len(s.wsClientStreams))
	for _, ch := range s.wsClientStreams {
		streams = append(streams, ch)
	}
	s.mu.RUnlock()

	for _, ch := range streams {
		select {
		case ch <- payload:
		default:
		}
	}
}
```

- [ ] **Step 4: Run gateway tests**

Run: `cd apps/gateway && go test ./internal/api -run "TestHandleSSEStreams" -v`

Expected: PASS (no regression; new helpers are unused yet)

- [ ] **Step 5: Commit**

```bash
cd E:\dev\webrtc-gateway
git add apps/gateway/internal/api/server.go apps/gateway/internal/api/ws_util.go
git commit -m "feat(gateway): add ws-client SSE stream subscribe/broadcast helpers"
```

---

### Task 3: Gateway — notifyWSClientChanged + handleWSClientsStream + route

**Files:**
- Modify: `apps/gateway/internal/api/handlers_sse.go` (add event type, notify fn, handler)
- Modify: `apps/gateway/internal/api/server.go:270` (register route)
- Modify: `apps/gateway/internal/api/handlers_api_test.go` (add stream test)

**Interfaces:**
- Consumes: `s.broadcastWSClientStream` from Task 2
- Produces: `s.notifyWSClientChanged(eventType string, client *WSClient)` — called by state-change hooks in Task 4

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

- [ ] **Step 3: Add WSClientStreamEvent + notifyWSClientChanged + handleWSClientsStream**

In `apps/gateway/internal/api/handlers_sse.go`, add after `SessionStreamEvent`:

```go
type WSClientStreamEvent struct {
	Type      string            `json:"type"`
	ClientID  string            `json:"clientId"`
	At        string            `json:"at"`
	Client    *WSClientResponse `json:"client,omitempty"`
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

Add `buildWSClientResponse` helper (mirrors handleListWSClients per-client logic) in `handlers_sse.go`:

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

- [ ] **Step 4: Register route**

In `apps/gateway/internal/api/server.go`, after `api.HandleFunc("/ws-clients", ...)` (line 270):

```go
		api.HandleFunc("/ws-clients/stream", s.handleWSClientsStream).Methods("GET", "OPTIONS")
```

- [ ] **Step 5: Run tests**

Run: `cd apps/gateway && go test ./internal/api -run "TestHandleWSClientsStream_Contract|TestHandleSSEStreams" -v`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd E:\dev\webrtc-gateway
git add apps/gateway/internal/api/handlers_sse.go apps/gateway/internal/api/server.go apps/gateway/internal/api/handlers_api_test.go
git commit -m "feat(gateway): add /ws-clients/stream SSE endpoint"
```

---

### Task 4: Gateway — hook notifyWSClientChanged at all state-change points

**Files:**
- Modify: `apps/gateway/internal/api/ws_conn.go:104-106` (after wsConnections register) + `:142-149` (disconnect)
- Modify: `apps/gateway/internal/api/ws_trunk.go:232-233` (after trunk resolve)
- Modify: `apps/gateway/internal/api/ws_midcall.go:132-135` (after client_state)
- Modify: `apps/gateway/internal/api/ws_call.go:63-65` (session register) + `:310-311` (trunk reset)
- Modify: `apps/gateway/internal/api/ws_resume.go:215` (register)
- Modify: `apps/gateway/internal/api/ws_incoming.go:518` (register)

**Interfaces:**
- Consumes: `s.notifyWSClientChanged` from Task 3
- Produces: SSE events emitted on every state change

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

- [ ] **Step 5: ws_resume.go — notify on register**

In `apps/gateway/internal/api/ws_resume.go`, after `s.wsClients[msg.SessionID] = client` (line 215):

```go
	s.wsClients[msg.SessionID] = client
	s.notifyWSClientChanged("updated", client)
```

- [ ] **Step 6: ws_incoming.go — notify on register**

In `apps/gateway/internal/api/ws_incoming.go`, after `s.wsClients[callSession.ID] = client` (line 518):

```go
	s.wsClients[callSession.ID] = client
	s.notifyWSClientChanged("updated", client)
```

- [ ] **Step 7: Run full gateway suite**

Run: `cd apps/gateway && go test ./...`

Expected: PASS

- [ ] **Step 8: Commit**

```bash
cd E:\dev\webrtc-gateway
git add apps/gateway/internal/api/ws_conn.go apps/gateway/internal/api/ws_trunk.go apps/gateway/internal/api/ws_midcall.go apps/gateway/internal/api/ws_call.go apps/gateway/internal/api/ws_resume.go apps/gateway/internal/api/ws_incoming.go
git commit -m "feat(gateway): emit ws-client SSE events on state changes"
```

---

### Task 5: Frontend — WS clients types + API service

**Files:**
- Create: `apps/frontend/src/features/ws-clients/types.ts`
- Create: `apps/frontend/src/features/ws-clients/services/ws-clients-api.ts`
- Create: `apps/frontend/src/features/ws-clients/services/ws-clients-api.test.ts`

**Interfaces:**
- Consumes: `fetchJson`, `resolveGatewayApiBaseUrl` from `@/lib/http-client`; `subscribeAuthenticatedSse` from `@/lib/sse-subscriber`
- Produces:
  - `fetchWSClients(): Promise<Array<WSClient>>`
  - `subscribeWSClientEvents(onEvent, onError?): () => void`

- [ ] **Step 1: Create types**

```typescript
// apps/frontend/src/features/ws-clients/types.ts
export interface WSClient {
  clientId: string
  sessionId?: string
  connectedAt: string
  trunkResolved: boolean
  resolvedTrunkId?: number
  resolvedTrunkPublicId?: string
  availability?: string
  callState?: string
  authSubject?: string
  publicOnly?: boolean
}

export interface WSClientStreamEvent {
  type: string
  clientId: string
  at: string
  client?: WSClient
}
```

- [ ] **Step 2: Write failing test**

```typescript
// apps/frontend/src/features/ws-clients/services/ws-clients-api.test.ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchWSClients } from './ws-clients-api'

describe('ws-clients-api', () => {
  afterEach(() => vi.restoreAllMocks())

  it('fetches ws clients list', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ 'content-type': 'application/json' }),
      json: () => [{ clientId: 'c1', trunkResolved: true, resolvedTrunkId: 5 }],
    })
    vi.stubGlobal('fetch', fetchMock)
    const result = await fetchWSClients()
    expect(result).toHaveLength(1)
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain('/ws-clients')
  })
})
```

- [ ] **Step 3: Implement API service**

```typescript
// apps/frontend/src/features/ws-clients/services/ws-clients-api.ts
import type { WSClient, WSClientStreamEvent } from '../types'
import { fetchJson, resolveGatewayApiBaseUrl } from '@/lib/http-client'
import { subscribeAuthenticatedSse } from '@/lib/sse-subscriber'

const API_BASE = resolveGatewayApiBaseUrl()

export async function fetchWSClients(): Promise<Array<WSClient>> {
  return fetchJson<Array<WSClient>>(`${API_BASE}/ws-clients`)
}

export function subscribeWSClientEvents(
  onEvent: (event: WSClientStreamEvent) => void,
  onError?: (event: Event) => void,
) {
  return subscribeAuthenticatedSse<WSClientStreamEvent>({
    url: `${API_BASE}/ws-clients/stream`,
    eventName: 'ws-client',
    onEvent,
    onError,
  })
}
```

- [ ] **Step 4: Run tests**

Run: `pnpm --filter frontend run test -- src/features/ws-clients/services/ws-clients-api.test.ts`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd E:\dev\webrtc-gateway
git add apps/frontend/src/features/ws-clients/
git commit -m "feat(frontend): add ws-clients API service and types"
```

---

### Task 6: Frontend — WS clients page component

**Files:**
- Create: `apps/frontend/src/features/ws-clients/components/ws-clients-page.tsx`

**Interfaces:**
- Consumes: `fetchWSClients`, `subscribeWSClientEvents` from Task 5; `hangupSession`, `sendSessionDtmf` from `@/features/active-sessions/services/session-control-api`
- Produces: `WSClientsPage` React component

- [ ] **Step 1: Implement the page**

```tsx
// apps/frontend/src/features/ws-clients/components/ws-clients-page.tsx
import {
  RiLoader4Line,
  RiMoonLine,
  RiPhoneLine,
  RiRefreshLine,
  RiSunLine,
} from '@remixicon/react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'

import type { WSClient, WSClientStreamEvent } from '../types'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DataTable } from '@/components/ui/data-table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import Header from '@/components/Header'
import { useTheme } from '@/lib/theme'
import { useVisibilityRealtimeReload } from '@/lib/use-visibility-realtime-reload'
import {
  fetchWSClients,
  subscribeWSClientEvents,
} from '../services/ws-clients-api'
import {
  hangupSession,
  sendSessionDtmf,
} from '@/features/active-sessions/services/session-control-api'

const DTMF_PATTERN = /^[0-9*#]+$/
const DTMF_MAX_LEN = 32
const ACTIVE_CALL_STATES = new Set(['incall', 'connecting', 'ringing'])

export function WSClientsPage() {
  const { theme, toggleTheme } = useTheme()
  const [clients, setClients] = useState<Array<WSClient>>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [actionLoadingId, setActionLoadingId] = useState<string | null>(null)
  const [dtmfClient, setDtmfClient] = useState<WSClient | null>(null)
  const [dtmfDigits, setDtmfDigits] = useState('')
  const [dtmfSubmitting, setDtmfSubmitting] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await fetchWSClients()
      setClients(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch WS clients')
    } finally {
      setLoading(false)
    }
  }, [])

  const loadRef = useRef(load)
  loadRef.current = load

  useEffect(() => {
    void load()
  }, [load])

  const handleSseEvent = useCallback((event: WSClientStreamEvent) => {
    setClients((prev) => {
      if (event.type === 'disconnected') {
        return prev.filter((c) => c.clientId !== event.clientId)
      }
      if (event.client) {
        const idx = prev.findIndex((c) => c.clientId === event.clientId)
        if (idx === -1) return [...prev, event.client]
        const next = [...prev]
        next[idx] = event.client
        return next
      }
      return prev
    })
  }, [])

  useVisibilityRealtimeReload({
    subscribe: subscribeWSClientEvents,
    onReload: () => void loadRef.current(),
  })

  // Override: use SSE directly instead of visibility reload's subscribe
  useEffect(() => {
    const unsubscribe = subscribeWSClientEvents(handleSseEvent, () => {
      void loadRef.current()
    })
    return unsubscribe
  }, [handleSseEvent])

  const handleHangup = useCallback(
    async (sessionId: string) => {
      if (
        !window.confirm(`Hang up session ${sessionId}? This sends SIP BYE via the gateway.`)
      ) {
        return
      }
      setActionError(null)
      setActionLoadingId(sessionId)
      try {
        await hangupSession(sessionId)
      } catch (err) {
        setActionError(err instanceof Error ? err.message : 'Failed to hang up session')
      } finally {
        setActionLoadingId(null)
      }
    },
    [],
  )

  const handleOpenDtmf = useCallback((client: WSClient) => {
    setActionError(null)
    setDtmfDigits('')
    setDtmfClient(client)
  }, [])

  const handleSubmitDtmf = useCallback(async () => {
    if (!dtmfClient?.sessionId) return
    const digits = dtmfDigits.trim()
    if (!digits || !DTMF_PATTERN.test(digits) || digits.length > DTMF_MAX_LEN) {
      setActionError('DTMF must be 1–32 characters: digits 0-9, * or # only')
      return
    }
    setActionError(null)
    setDtmfSubmitting(true)
    try {
      await sendSessionDtmf(dtmfClient.sessionId, digits)
      setDtmfClient(null)
      setDtmfDigits('')
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to send DTMF')
    } finally {
      setDtmfSubmitting(false)
    }
  }, [dtmfClient, dtmfDigits])

  const columns = useMemo<Array<ColumnDef<WSClient>>>(
    () => [
      {
        accessorKey: 'clientId',
        header: 'Client ID',
        cell: ({ row }) => (
          <span className="font-mono text-xs">{row.original.clientId.slice(0, 8)}…</span>
        ),
      },
      {
        accessorKey: 'sessionId',
        header: 'Session',
        cell: ({ row }) => (
          <span className="font-mono text-xs text-muted-foreground">
            {row.original.sessionId || '-'}
          </span>
        ),
      },
      {
        id: 'trunk',
        header: 'Trunk',
        cell: ({ row }) => {
          const c = row.original
          if (!c.trunkResolved) {
            return <Badge variant="outline" className="text-[10px]">unresolved</Badge>
          }
          const label = c.resolvedTrunkPublicId || `#${c.resolvedTrunkId}`
          return (
            <Badge variant="success" className="text-[10px] font-mono">
              {label}
            </Badge>
          )
        },
      },
      {
        accessorKey: 'availability',
        header: 'Availability',
        cell: ({ row }) => {
          const a = row.original.availability
          let variant: 'default' | 'success' | 'warning' | 'destructive' = 'default'
          if (a === 'idle') variant = 'success'
          else if (a === 'busy') variant = 'warning'
          else if (a === 'unavailable') variant = 'destructive'
          return (
            <Badge variant={variant} className="text-[10px]">
              {a || '-'}
            </Badge>
          )
        },
      },
      {
        accessorKey: 'callState',
        header: 'Call State',
        cell: ({ row }) => (
          <span className="text-xs">{row.original.callState || '-'}</span>
        ),
      },
      {
        accessorKey: 'authSubject',
        header: 'Auth',
        cell: ({ row }) => (
          <span className="truncate text-xs text-muted-foreground">
            {row.original.authSubject || '-'}
          </span>
        ),
      },
      {
        id: 'actions',
        header: () => <div className="text-right">Actions</div>,
        cell: ({ row }) => {
          const c = row.original
          const isActive = !!c.sessionId && ACTIVE_CALL_STATES.has(c.callState ?? '')
          if (!isActive) return null
          const busy = actionLoadingId === c.sessionId
          return (
            <div className="flex justify-end gap-1">
              <Button
                size="sm"
                variant="secondary"
                className="h-6 px-2 text-[10px]"
                onClick={() => handleOpenDtmf(c)}
              >
                DTMF
              </Button>
              <Button
                size="sm"
                variant="destructive"
                className="h-6 px-2 text-[10px]"
                disabled={busy}
                onClick={() => void handleHangup(c.sessionId!)}
              >
                {busy ? '…' : 'Hangup'}
              </Button>
            </div>
          )
        },
      },
    ],
    [actionLoadingId, handleHangup, handleOpenDtmf],
  )

  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <Header>
        <div className="flex items-center gap-2 text-xs">
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => void load()}
            disabled={loading}
          >
            <RiRefreshLine className={`size-3.5 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </Button>
          <Separator orientation="vertical" className="h-4" />
          <Button
            size="icon"
            variant="ghost"
            className="size-7"
            onClick={toggleTheme}
            aria-label={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
          >
            {theme === 'dark' ? <RiSunLine className="size-3.5" /> : <RiMoonLine className="size-3.5" />}
          </Button>
        </div>
      </Header>

      <div className="flex-1 overflow-y-auto p-4">
        {error ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-400">
            {error}
          </div>
        ) : null}
        {actionError ? (
          <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-400">
            {actionError}
          </div>
        ) : null}
        {loading && clients.length === 0 ? (
          <div className="flex justify-center py-20">
            <RiLoader4Line className="size-6 animate-spin text-muted-foreground" />
          </div>
        ) : clients.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-20 text-muted-foreground">
            <RiPhoneLine className="size-10 opacity-30" />
            <p className="text-sm">No connected WebSocket clients</p>
          </div>
        ) : (
          <DataTable columns={columns} data={clients} />
        )}
      </div>

      <Dialog
        open={dtmfClient !== null}
        onOpenChange={(open) => {
          if (!open) {
            setDtmfClient(null)
            setDtmfDigits('')
          }
        }}
      >
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Send DTMF</DialogTitle>
            <DialogDescription className="font-mono text-xs">
              Session {dtmfClient?.sessionId}
            </DialogDescription>
          </DialogHeader>
          <Input
            value={dtmfDigits}
            onChange={(e) => setDtmfDigits(e.target.value)}
            placeholder="e.g. 123#"
            className="font-mono text-sm"
            maxLength={DTMF_MAX_LEN}
          />
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setDtmfClient(null)}>
              Cancel
            </Button>
            <Button size="sm" disabled={dtmfSubmitting} onClick={() => void handleSubmitDtmf()}>
              {dtmfSubmitting ? 'Sending…' : 'Send'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
```

- [ ] **Step 2: Commit**

```bash
cd E:\dev\webrtc-gateway
git add apps/frontend/src/features/ws-clients/components/ws-clients-page.tsx
git commit -m "feat(frontend): add ws-clients real-time page component"
```

---

### Task 7: Frontend — route + menu entry

**Files:**
- Create: `apps/frontend/src/routes/ws-clients.tsx`
- Modify: `apps/frontend/src/components/Header.tsx:220-233` (add menu link between Active Sessions and Gateway Logs)
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

In `apps/frontend/src/components/Header.tsx`, after the Active Sessions link (around line 220), add:

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
                  <RiPulseLine size={16} />
                  <span className="font-medium">WS Clients</span>
                </Link>
```

Add `RiRouterLine` to the remixicon import (or reuse `RiPulseLine`). Use `RiRouterLine`:

Add to the remixicon import block at top:
```tsx
  RiRouterLine,
```

And use `<RiRouterLine size={16} />` in the link.

- [ ] **Step 3: Regenerate route tree**

Run: `pnpm dev:frontend` — start, wait 3s for route tree to regenerate, then Ctrl+C.

Verify: `apps/frontend/src/routeTree.gen.ts` contains `/ws-clients`.

- [ ] **Step 4: Run frontend tests + check**

Run: `pnpm --filter frontend run test && pnpm --filter frontend run check`

Expected: tests PASS; check may have pre-existing eslint errors in unrelated files but no NEW errors in ws-clients files.

- [ ] **Step 5: Commit**

```bash
cd E:\dev\webrtc-gateway
git add apps/frontend/src/routes/ws-clients.tsx apps/frontend/src/components/Header.tsx apps/frontend/src/routeTree.gen.ts
git commit -m "feat(frontend): add /ws-clients route and menu entry"
```

---

### Task 8: Final verification

**Files:** none (verification only)

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

- [ ] **Step 3: Update docs**

Add to `apps/gateway/AGENTS.md` section 5 file map, after `handlers_sse.go` row:
```
| `handlers_sse.go` | trunk/session/ws-client SSE streams |
```

Add to `docs/gateway/ops-guide.md`:
```
- `GET /api/ws-clients/stream` — SSE stream of WS client connect/disconnect/update events (each event carries the full `WSClientResponse`).
```

- [ ] **Step 4: Commit**

```bash
cd E:\dev\webrtc-gateway
git add apps/gateway/AGENTS.md docs/gateway/ops-guide.md
git commit -m "docs: document /ws-clients/stream SSE endpoint"
```

---

## Self-review

**Spec coverage:**
- SSE stream new → Task 2, 3, 4 ✓
- New page → Task 6, 7 ✓
- Columns full (Session, Trunk, Auth, Availability, Call state, Connected, Actions) → Task 6 ✓
- Actions only active → Task 6 `ACTIVE_CALL_STATES` check ✓
- Show idle trunk-resolved clients → Task 1 (iterate `wsConnections` instead of `wsClients`) ✓
- Fallback poll → Task 6 SSE onError → `loadRef.current()` ✓

**Placeholder scan:** No TBD/TODO; all steps have complete code.

**Type consistency:** `WSClient` / `WSClientResponse` / `WSClientStreamEvent` field names match across gateway (Go JSON tags) and frontend (TS interface). `clientId` (camelCase JSON) matches both sides. `hangupSession` / `sendSessionDtmf` signatures match Task 1 of the previous plan (already implemented and committed).

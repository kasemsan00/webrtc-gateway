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

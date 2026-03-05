package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"k2-gateway/internal/config"
	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
	"k2-gateway/internal/sip"
)

type stubResolveStore struct {
	logstore.LogStore
	resolveTrunkID    int64
	resolveLeaseOwner *string
	resolveLeaseUntil *time.Time
	resolveFound      bool
	resolveErr        error
	lookupWSURL       string
	lookupFound       bool
	lookupErr         error
}

type stubResolveTrunkManager struct {
	byID       map[int64]*sip.Trunk
	byPublicID map[string]int64
}

func (s *stubResolveTrunkManager) GetTrunkByID(id int64) (interface{}, bool) {
	t, ok := s.byID[id]
	return t, ok
}
func (s *stubResolveTrunkManager) GetTrunkByPublicID(publicID string) (interface{}, bool) {
	id, ok := s.byPublicID[publicID]
	if !ok {
		return nil, false
	}
	return s.GetTrunkByID(id)
}
func (s *stubResolveTrunkManager) GetTrunkIDByPublicID(publicID string) (int64, bool) {
	id, ok := s.byPublicID[publicID]
	return id, ok
}
func (s *stubResolveTrunkManager) GetDefaultTrunk() (interface{}, bool) { return nil, false }
func (s *stubResolveTrunkManager) RefreshTrunks() error                 { return nil }
func (s *stubResolveTrunkManager) CreateTrunk(ctx context.Context, payload sip.CreateTrunkPayload) (*sip.Trunk, error) {
	return nil, errors.New("not implemented")
}
func (s *stubResolveTrunkManager) UpdateTrunk(ctx context.Context, trunkID int64, patch sip.TrunkUpdatePatch) (*sip.Trunk, error) {
	return nil, errors.New("not implemented")
}
func (s *stubResolveTrunkManager) RegisterTrunk(trunkID int64, force bool) error {
	return errors.New("not implemented")
}
func (s *stubResolveTrunkManager) UnregisterTrunk(trunkID int64, force bool) error {
	return errors.New("not implemented")
}
func (s *stubResolveTrunkManager) DeleteTrunk(trunkID int64, force bool) error {
	return errors.New("not implemented")
}
func (s *stubResolveTrunkManager) ListTrunks(ctx context.Context, params sip.TrunkListParams) (*sip.TrunkListResult, error) {
	return nil, errors.New("not implemented")
}
func (s *stubResolveTrunkManager) GetTrunkByIDFromDB(ctx context.Context, trunkID int64) (*sip.Trunk, error) {
	t, ok := s.byID[trunkID]
	if !ok {
		return nil, errors.New("not found")
	}
	return t, nil
}
func (s *stubResolveTrunkManager) ListOwnedTrunks() []*sip.Trunk {
	items := make([]*sip.Trunk, 0, len(s.byID))
	for _, t := range s.byID {
		items = append(items, t)
	}
	return items
}

func (s *stubResolveStore) ResolveTrunkByCredentials(ctx context.Context, domain string, port int, username, password string) (int64, *string, *time.Time, bool, error) {
	return s.resolveTrunkID, s.resolveLeaseOwner, s.resolveLeaseUntil, s.resolveFound, s.resolveErr
}

func (s *stubResolveStore) LookupGatewayInstance(ctx context.Context, instanceID string) (string, bool, error) {
	return s.lookupWSURL, s.lookupFound, s.lookupErr
}

func readWSMessages(t *testing.T, ch <-chan []byte) []WSMessage {
	t.Helper()

	var msgs []WSMessage
	for {
		select {
		case raw := <-ch:
			var msg WSMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				t.Fatalf("failed to unmarshal websocket message: %v", err)
			}
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}

func TestHandleWSTrunkResolve_InvalidPayload(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, nil, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSTrunkResolve(client, WSMessage{Type: "trunk_resolve", SessionID: "s1"})
	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != "error" {
		t.Fatalf("expected error message, got %s", msgs[0].Type)
	}
}

func TestHandleWSTrunkResolve_NotFoundSendsTrunkNotFoundAndError(t *testing.T) {
	store := &stubResolveStore{resolveFound: false}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, nil, nil, nil, nil, store)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:        "trunk_resolve",
		SessionID:   "s1",
		SIPDomain:   "sip.example.com",
		SIPUsername: "1001",
		SIPPassword: "secret",
		SIPPort:     5060,
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Type != "trunk_not_found" {
		t.Fatalf("expected first message trunk_not_found, got %s", msgs[0].Type)
	}
	if msgs[1].Type != "error" {
		t.Fatalf("expected second message error, got %s", msgs[1].Type)
	}
	if client.trunkResolved {
		t.Fatalf("expected client.trunkResolved=false for trunk_not_found")
	}
}

func TestHandleWSTrunkResolve_LeaseNotActive(t *testing.T) {
	owner := "gw-1"
	past := time.Now().Add(-1 * time.Minute)
	store := &stubResolveStore{
		resolveTrunkID:    42,
		resolveLeaseOwner: &owner,
		resolveLeaseUntil: &past,
		resolveFound:      true,
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, nil, nil, nil, nil, store)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:        "trunk_resolve",
		SessionID:   "s1",
		SIPDomain:   "sip.example.com",
		SIPUsername: "1001",
		SIPPassword: "secret",
		SIPPort:     5060,
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != "trunk_not_ready" {
		t.Fatalf("expected trunk_not_ready, got %s", msgs[0].Type)
	}
	if client.trunkResolved {
		t.Fatalf("expected client.trunkResolved=false for trunk_not_ready")
	}
}

func TestHandleWSTrunkResolve_ResolvedWhenOwnedByInstance(t *testing.T) {
	owner := "gw-1"
	future := time.Now().Add(2 * time.Minute)
	store := &stubResolveStore{
		resolveTrunkID:    42,
		resolveLeaseOwner: &owner,
		resolveLeaseUntil: &future,
		resolveFound:      true,
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, nil, nil, nil, nil, store)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:        "trunk_resolve",
		SessionID:   "s1",
		SIPDomain:   "sip.example.com",
		SIPUsername: "1001",
		SIPPassword: "secret",
		SIPPort:     5060,
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != "trunk_resolved" {
		t.Fatalf("expected trunk_resolved, got %s", msgs[0].Type)
	}
	if msgs[0].TrunkID != 42 {
		t.Fatalf("expected trunkID=42, got %d", msgs[0].TrunkID)
	}
	if !client.trunkResolved {
		t.Fatalf("expected client.trunkResolved=true for trunk_resolved")
	}
}

func TestHandleWSTrunkResolve_ResolvedReplaysPendingIncoming(t *testing.T) {
	owner := "gw-1"
	future := time.Now().Add(2 * time.Minute)
	store := &stubResolveStore{
		resolveTrunkID:    42,
		resolveLeaseOwner: &owner,
		resolveLeaseUntil: &future,
		resolveFound:      true,
	}

	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "sip:linphone@example.com", "sip:agent@example.com", "sip-call-99")

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, mgr, nil, nil, nil, store)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:        "trunk_resolve",
		SessionID:   "s1",
		SIPDomain:   "sip.example.com",
		SIPUsername: "1001",
		SIPPassword: "secret",
		SIPPort:     5060,
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (trunk_resolved + incoming), got %d", len(msgs))
	}
	if msgs[0].Type != "trunk_resolved" {
		t.Fatalf("expected first message trunk_resolved, got %s", msgs[0].Type)
	}
	if msgs[1].Type != "incoming" {
		t.Fatalf("expected second message incoming, got %s", msgs[1].Type)
	}
	if msgs[1].SessionID != incomingSess.ID {
		t.Fatalf("expected incoming sessionID=%s, got %s", incomingSess.ID, msgs[1].SessionID)
	}
	if msgs[1].From != "sip:linphone@example.com" || msgs[1].To != "sip:agent@example.com" {
		t.Fatalf("unexpected incoming from/to: from=%s to=%s", msgs[1].From, msgs[1].To)
	}
	if !client.trunkResolved {
		t.Fatalf("expected client.trunkResolved=true after successful trunk_resolve")
	}
}

func TestHandleWSTrunkResolve_RedirectWhenOwnedByOtherInstance(t *testing.T) {
	owner := "gw-2"
	future := time.Now().Add(2 * time.Minute)
	store := &stubResolveStore{
		resolveTrunkID:    42,
		resolveLeaseOwner: &owner,
		resolveLeaseUntil: &future,
		resolveFound:      true,
		lookupWSURL:       "wss://gw-2.example.com/ws",
		lookupFound:       true,
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, nil, nil, nil, nil, store)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:        "trunk_resolve",
		SessionID:   "s1",
		SIPDomain:   "sip.example.com",
		SIPUsername: "1001",
		SIPPassword: "secret",
		SIPPort:     5060,
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != "trunk_redirect" {
		t.Fatalf("expected trunk_redirect, got %s", msgs[0].Type)
	}
	if client.trunkResolved {
		t.Fatalf("expected client.trunkResolved=false for trunk_redirect")
	}
}

func TestHandleWSTrunkResolve_ResolveFailure(t *testing.T) {
	store := &stubResolveStore{resolveErr: errors.New("db down")}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, nil, nil, nil, nil, store)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:        "trunk_resolve",
		SessionID:   "s1",
		SIPDomain:   "sip.example.com",
		SIPUsername: "1001",
		SIPPassword: "secret",
		SIPPort:     5060,
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != "error" {
		t.Fatalf("expected error, got %s", msgs[0].Type)
	}
	if client.trunkResolved {
		t.Fatalf("expected client.trunkResolved=false for resolve error")
	}
}

func TestHandleWSTrunkResolve_ByTrunkID_ResolvedWhenOwnedByInstance(t *testing.T) {
	owner := "gw-1"
	future := time.Now().Add(2 * time.Minute)
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {
				ID:         42,
				PublicID:   "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a",
				LeaseOwner: &owner,
				LeaseUntil: &future,
			},
		},
		byPublicID: map[string]int64{
			"8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a": 42,
		},
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:      "trunk_resolve",
		SessionID: "s1",
		TrunkID:   42,
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "trunk_resolved" {
		t.Fatalf("expected trunk_resolved, got %+v", msgs)
	}
	if !client.trunkResolved {
		t.Fatalf("expected client.trunkResolved=true for trunk_resolved by trunkId")
	}
}

func TestHandleWSTrunkResolve_ByTrunkPublicID_ResolvedWhenOwnedByInstance(t *testing.T) {
	owner := "gw-1"
	future := time.Now().Add(2 * time.Minute)
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {
				ID:         42,
				PublicID:   "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a",
				LeaseOwner: &owner,
				LeaseUntil: &future,
			},
		},
		byPublicID: map[string]int64{
			"8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a": 42,
		},
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:          "trunk_resolve",
		SessionID:     "s1",
		TrunkPublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a",
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "trunk_resolved" {
		t.Fatalf("expected trunk_resolved, got %+v", msgs)
	}
	if !client.trunkResolved {
		t.Fatalf("expected client.trunkResolved=true for trunk_resolved by trunkPublicId")
	}
}

func TestHandleWSTrunkResolve_ByTrunkID_NotReadyWhenLeaseExpired(t *testing.T) {
	owner := "gw-1"
	past := time.Now().Add(-1 * time.Minute)
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {
				ID:         42,
				PublicID:   "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a",
				LeaseOwner: &owner,
				LeaseUntil: &past,
			},
		},
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:      "trunk_resolve",
		SessionID: "s1",
		TrunkID:   42,
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "trunk_not_ready" {
		t.Fatalf("expected trunk_not_ready, got %+v", msgs)
	}
	if client.trunkResolved {
		t.Fatalf("expected client.trunkResolved=false for trunk_not_ready by trunkId")
	}
}

func TestHandleWSTrunkResolve_ByTrunkID_RedirectWhenOwnedByOtherInstance(t *testing.T) {
	owner := "gw-2"
	future := time.Now().Add(2 * time.Minute)
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {
				ID:         42,
				PublicID:   "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a",
				LeaseOwner: &owner,
				LeaseUntil: &future,
			},
		},
	}
	store := &stubResolveStore{
		lookupWSURL: "wss://gw-2.example.com/ws",
		lookupFound: true,
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, nil, nil, nil, trunkMgr, store)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:      "trunk_resolve",
		SessionID: "s1",
		TrunkID:   42,
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "trunk_redirect" {
		t.Fatalf("expected trunk_redirect, got %+v", msgs)
	}
	if client.trunkResolved {
		t.Fatalf("expected client.trunkResolved=false for trunk_redirect by trunkId")
	}
}

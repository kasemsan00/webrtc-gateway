package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"k2-gateway/internal/config"
	"k2-gateway/internal/logstore"
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
}

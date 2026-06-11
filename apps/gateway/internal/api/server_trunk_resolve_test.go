package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"k2-gateway/internal/auth"
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
	byID            map[int64]*sip.Trunk
	byPublicID      map[string]int64
	lookupCount     int
	pushContact     *sip.TrunkPushContact
	pushTrunkID     int64
	registerID      int64
	notifyTrunkID   int64
	notifyUserID    *string
	notifyPlatform  *string
	notifyCallCount int
	notifyErr       error
	pushChanged     bool
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
	s.lookupCount++
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
	s.registerID = trunkID
	return nil
}
func (s *stubResolveTrunkManager) UnregisterTrunk(trunkID int64, force bool) error {
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

func (s *stubResolveTrunkManager) SetTrunkInUseBy(_ context.Context, _ int64, _ *string) error {
	return nil
}

func (s *stubResolveTrunkManager) FindTrunkByInUseBy(_ context.Context, _ string) (*sip.Trunk, error) {
	return nil, nil
}

func (s *stubResolveTrunkManager) SetTrunkNotifyUserID(_ context.Context, trunkID int64, userID *string) error {
	s.notifyTrunkID = trunkID
	if userID == nil {
		s.notifyUserID = nil
	} else {
		copied := *userID
		s.notifyUserID = &copied
	}
	s.notifyCallCount++
	return s.notifyErr
}

func (s *stubResolveTrunkManager) SetTrunkNotifyUserIDAndPlatform(_ context.Context, trunkID int64, userID *string, platform *string) error {
	s.notifyTrunkID = trunkID
	if userID == nil {
		s.notifyUserID = nil
	} else {
		copied := *userID
		s.notifyUserID = &copied
	}
	if platform == nil {
		s.notifyPlatform = nil
	} else {
		copied := *platform
		s.notifyPlatform = &copied
	}
	s.notifyCallCount++
	return s.notifyErr
}

func (s *stubResolveTrunkManager) SetTrunkPushContact(_ context.Context, trunkID int64, contact sip.TrunkPushContact) (bool, error) {
	s.pushTrunkID = trunkID
	s.pushContact = &contact
	trunk, ok := s.byID[trunkID]
	if ok && trunk.PNAppID != nil && trunk.PNType != nil && trunk.PNToken != nil &&
		strings.TrimSpace(*trunk.PNAppID) == strings.TrimSpace(contact.PNAppID) &&
		strings.TrimSpace(*trunk.PNType) == strings.TrimSpace(contact.PNType) &&
		strings.TrimSpace(*trunk.PNToken) == strings.TrimSpace(contact.PNToken) {
		s.pushChanged = false
		return false, nil
	}
	s.pushChanged = true
	return true, nil
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

func captureStandardLogs(t *testing.T, fn func()) string {
	t.Helper()

	var buf bytes.Buffer
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	prevPrefix := log.Prefix()
	defer func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
		log.SetPrefix(prevPrefix)
	}()

	log.SetOutput(&buf)
	log.SetFlags(0)
	log.SetPrefix("")

	fn()
	return buf.String()
}

func TestHandleWSTrunkResolve_InvalidPayload(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
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
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, nil, store)
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
	if client.resolvedTrunkID != 0 {
		t.Fatalf("expected client.resolvedTrunkID=0 for trunk_not_found, got %d", client.resolvedTrunkID)
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

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, nil, store)
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
	if client.resolvedTrunkID != 0 {
		t.Fatalf("expected client.resolvedTrunkID=0 for trunk_not_ready, got %d", client.resolvedTrunkID)
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

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, nil, store)
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
	if client.resolvedTrunkID != 42 {
		t.Fatalf("expected client.resolvedTrunkID=42 for trunk_resolved, got %d", client.resolvedTrunkID)
	}
}

func TestHandleWSTrunkResolve_ByCredentials_PersistsNotifyUserID(t *testing.T) {
	owner := "gw-1"
	future := time.Now().Add(2 * time.Minute)
	store := &stubResolveStore{
		resolveTrunkID:    42,
		resolveLeaseOwner: &owner,
		resolveLeaseUntil: &future,
		resolveFound:      true,
	}
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

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, store)
	client := &WSClient{
		send:       make(chan []byte, 8),
		authClaims: &auth.VerifiedClaims{Subject: "user-1"},
	}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:        "trunk_resolve",
		SessionID:   "s1",
		SIPDomain:   "sip.example.com",
		SIPUsername: "1001",
		SIPPassword: "secret",
		SIPPort:     5060,
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "trunk_resolved" {
		t.Fatalf("expected trunk_resolved, got %+v", msgs)
	}
	if trunkMgr.notifyCallCount != 1 {
		t.Fatalf("expected notify_user_id update once, got %d", trunkMgr.notifyCallCount)
	}
	if trunkMgr.notifyTrunkID != 42 {
		t.Fatalf("expected notify_user_id update for resolved trunk 42, got %d", trunkMgr.notifyTrunkID)
	}
	if trunkMgr.notifyUserID == nil || *trunkMgr.notifyUserID != "user-1" {
		t.Fatalf("expected notify_user_id user-1, got %#v", trunkMgr.notifyUserID)
	}
}

func TestHandleWSTrunkResolve_ByCredentials_PersistsDevicePlatform(t *testing.T) {
	owner := "gw-1"
	future := time.Now().Add(2 * time.Minute)
	store := &stubResolveStore{
		resolveTrunkID:    42,
		resolveLeaseOwner: &owner,
		resolveLeaseUntil: &future,
		resolveFound:      true,
	}
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

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, store)
	client := &WSClient{
		send:       make(chan []byte, 8),
		authClaims: &auth.VerifiedClaims{Subject: "user-1"},
	}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:           "trunk_resolve",
		SessionID:      "s1",
		SIPDomain:      "sip.example.com",
		SIPUsername:    "1001",
		SIPPassword:    "secret",
		SIPPort:        5060,
		DevicePlatform: "android",
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "trunk_resolved" {
		t.Fatalf("expected trunk_resolved, got %+v", msgs)
	}
	if trunkMgr.notifyPlatform == nil || *trunkMgr.notifyPlatform != "android" {
		t.Fatalf("expected platform android, got %#v", trunkMgr.notifyPlatform)
	}
}

func TestHandleWSTrunkResolve_InvalidDevicePlatformRejected(t *testing.T) {
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
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{
		send:       make(chan []byte, 8),
		authClaims: &auth.VerifiedClaims{Subject: "user-1"},
	}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:           "trunk_resolve",
		SessionID:      "s1",
		TrunkID:        42,
		DevicePlatform: "windows",
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "error" {
		t.Fatalf("expected error, got %+v", msgs)
	}
	if trunkMgr.notifyCallCount != 0 {
		t.Fatalf("expected no notify/platform update, got %d", trunkMgr.notifyCallCount)
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
	incomingSess.SetSIPAuthContext("trunk", "", 42, "sip.example.com", "1001", "secret", 5060)
	incomingSess.SetIncomingInvite(nil, nil, []byte("v=0\r\nm=audio 4000 RTP/AVP 111\r\nm=video 4002 RTP/AVP 96\r\na=sendrecv\r\n"), "sip:linphone@example.com", "sip:agent@example.com")

	otherIncoming, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create other incoming session: %v", err)
	}
	otherIncoming.SetState(session.StateIncoming)
	otherIncoming.SetCallInfo("inbound", "sip:other@example.com", "sip:agent@example.com", "sip-call-100")
	otherIncoming.SetSIPAuthContext("trunk", "", 99, "sip.example.com", "1002", "secret", 5060)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, mgr, nil, nil, nil, store)
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
		t.Fatalf("expected 2 messages (trunk_resolved + matching incoming), got %d", len(msgs))
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
	if msgs[1].HasVideo != "true" {
		t.Fatalf("expected replayed incoming hasVideo=true, got %q", msgs[1].HasVideo)
	}
	if !client.trunkResolved {
		t.Fatalf("expected client.trunkResolved=true after successful trunk_resolve")
	}
	if client.resolvedTrunkID != 42 {
		t.Fatalf("expected client.resolvedTrunkID=42 after successful trunk_resolve, got %d", client.resolvedTrunkID)
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

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, nil, store)
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
	if client.resolvedTrunkID != 0 {
		t.Fatalf("expected client.resolvedTrunkID=0 for trunk_redirect, got %d", client.resolvedTrunkID)
	}
}

func TestHandleWSTrunkResolve_ResolveFailure(t *testing.T) {
	store := &stubResolveStore{resolveErr: errors.New("db down")}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, nil, store)
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
	if client.resolvedTrunkID != 0 {
		t.Fatalf("expected client.resolvedTrunkID=0 for resolve error, got %d", client.resolvedTrunkID)
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

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{
		send:       make(chan []byte, 8),
		authClaims: &auth.VerifiedClaims{Subject: "user-1"},
	}

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
	if client.resolvedTrunkID != 42 {
		t.Fatalf("expected client.resolvedTrunkID=42 for trunk_resolved by trunkId, got %d", client.resolvedTrunkID)
	}
	if trunkMgr.notifyCallCount != 1 {
		t.Fatalf("expected notify_user_id update once, got %d", trunkMgr.notifyCallCount)
	}
	if trunkMgr.notifyTrunkID != 42 {
		t.Fatalf("expected notify_user_id update for trunk 42, got %d", trunkMgr.notifyTrunkID)
	}
	if trunkMgr.notifyUserID == nil || *trunkMgr.notifyUserID != "user-1" {
		t.Fatalf("expected notify_user_id user-1, got %#v", trunkMgr.notifyUserID)
	}
}

func TestHandleWSTrunkResolve_ByTrunkID_DoesNotPersistNotifyUserIDWithoutAuthClaims(t *testing.T) {
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
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
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
	if trunkMgr.notifyCallCount != 0 {
		t.Fatalf("expected no notify_user_id update without auth claims, got %d", trunkMgr.notifyCallCount)
	}
}

func TestHandleWSTrunkResolve_WithPushContact_PersistsAndReregisters(t *testing.T) {
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
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{
		send:       make(chan []byte, 8),
		authClaims: &auth.VerifiedClaims{Subject: "user-1"},
	}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:      "trunk_resolve",
		SessionID: "s1",
		TrunkID:   42,
		PNAppID:   config.DefaultTrunkPNAppID,
		PNType:    "apple",
		PNToken:   "D6F5DF83B03398129B4AC01DFE5971662B46130F3F5424AF93CF0A8C02A74CCF",
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "trunk_resolved" {
		t.Fatalf("expected trunk_resolved only, got %+v", msgs)
	}
	if trunkMgr.pushTrunkID != 42 {
		t.Fatalf("expected push contact for trunk 42, got %d", trunkMgr.pushTrunkID)
	}
	if trunkMgr.pushContact == nil || trunkMgr.pushContact.PNAppID != config.DefaultTrunkPNAppID || trunkMgr.pushContact.PNType != "apple" {
		t.Fatalf("unexpected push contact: %+v", trunkMgr.pushContact)
	}
	if trunkMgr.registerID != 42 {
		t.Fatalf("expected re-register for trunk 42, got %d", trunkMgr.registerID)
	}
}

func TestHandleWSTrunkResolve_WithUnchangedPushContact_SkipsReregister(t *testing.T) {
	owner := "gw-1"
	future := time.Now().Add(2 * time.Minute)
	appID := config.DefaultTrunkPNAppID
	pnType := "apple"
	pnToken := "D6F5DF83B03398129B4AC01DFE5971662B46130F3F5424AF93CF0A8C02A74CCF"
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {
				ID:         42,
				PublicID:   "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a",
				LeaseOwner: &owner,
				LeaseUntil: &future,
				PNAppID:    &appID,
				PNType:     &pnType,
				PNToken:    &pnToken,
			},
		},
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{
		send:       make(chan []byte, 8),
		authClaims: &auth.VerifiedClaims{Subject: "user-1"},
	}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:      "trunk_resolve",
		SessionID: "s1",
		TrunkID:   42,
		PNAppID:   " " + config.DefaultTrunkPNAppID + " ",
		PNType:    "apple",
		PNToken:   " " + pnToken + " ",
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "trunk_resolved" {
		t.Fatalf("expected trunk_resolved only, got %+v", msgs)
	}
	if trunkMgr.pushTrunkID != 42 {
		t.Fatalf("expected push contact persist attempt for trunk 42, got %d", trunkMgr.pushTrunkID)
	}
	if trunkMgr.registerID != 0 {
		t.Fatalf("expected unchanged push contact to skip re-register, got register trunk %d", trunkMgr.registerID)
	}
}

func TestHandleWSTrunkPushToken_RequiresResolvedMatchingTrunk(t *testing.T) {
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {ID: 42, PublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a"},
		},
		byPublicID: map[string]int64{
			"8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a": 42,
		},
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{
		send:            make(chan []byte, 8),
		trunkResolved:   true,
		resolvedTrunkID: 42,
		authClaims:      &auth.VerifiedClaims{Subject: "user-1"},
	}

	srv.handleWSTrunkPushToken(client, WSMessage{
		Type:          "trunk_push_token",
		TrunkPublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a",
		PNAppID:       config.DefaultTrunkPNAppID,
		PNType:        "apple",
		PNToken:       "D6F5DF83B03398129B4AC01DFE5971662B46130F3F5424AF93CF0A8C02A74CCF",
	})

	if msgs := readWSMessages(t, client.send); len(msgs) != 0 {
		t.Fatalf("expected no error messages, got %+v", msgs)
	}
	if trunkMgr.pushTrunkID != 42 || trunkMgr.registerID != 42 {
		t.Fatalf("expected persist and re-register for trunk 42, push=%d register=%d", trunkMgr.pushTrunkID, trunkMgr.registerID)
	}

	srv.handleWSTrunkPushToken(client, WSMessage{
		Type:    "trunk_push_token",
		TrunkID: 99,
		PNAppID: config.DefaultTrunkPNAppID,
		PNType:  "apple",
		PNToken: "D6F5DF83B03398129B4AC01DFE5971662B46130F3F5424AF93CF0A8C02A74CCF",
	})
	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "error" {
		t.Fatalf("expected mismatch error, got %+v", msgs)
	}
}

func TestHandleWSTrunkPushToken_UnchangedContactSkipsReregister(t *testing.T) {
	appID := config.DefaultTrunkPNAppID
	pnType := "apple"
	pnToken := "D6F5DF83B03398129B4AC01DFE5971662B46130F3F5424AF93CF0A8C02A74CCF"
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {
				ID:       42,
				PublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a",
				PNAppID:  &appID,
				PNType:   &pnType,
				PNToken:  &pnToken,
			},
		},
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{
		send:            make(chan []byte, 8),
		trunkResolved:   true,
		resolvedTrunkID: 42,
		authClaims:      &auth.VerifiedClaims{Subject: "user-1"},
	}

	srv.handleWSTrunkPushToken(client, WSMessage{
		Type:    "trunk_push_token",
		TrunkID: 42,
		PNAppID: " " + config.DefaultTrunkPNAppID + " ",
		PNType:  "apple",
		PNToken: " " + pnToken + " ",
	})

	if msgs := readWSMessages(t, client.send); len(msgs) != 0 {
		t.Fatalf("expected no error messages, got %+v", msgs)
	}
	if trunkMgr.pushTrunkID != 42 {
		t.Fatalf("expected push contact persist attempt for trunk 42, got %d", trunkMgr.pushTrunkID)
	}
	if trunkMgr.registerID != 0 {
		t.Fatalf("expected unchanged push contact to skip re-register, got register trunk %d", trunkMgr.registerID)
	}
}

func TestHandleWSTrunkPushToken_UsesConfiguredPNAppID(t *testing.T) {
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {ID: 42, PublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a"},
		},
	}
	srv := NewServer(config.APIConfig{TrunkPNAppID: "th.or.ttrs.video.staging"}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{
		send:            make(chan []byte, 8),
		trunkResolved:   true,
		resolvedTrunkID: 42,
		authClaims:      &auth.VerifiedClaims{Subject: "user-1"},
	}

	srv.handleWSTrunkPushToken(client, WSMessage{
		Type:    "trunk_push_token",
		TrunkID: 42,
		PNAppID: "th.or.ttrs.video.staging",
		PNType:  "apple",
		PNToken: "D6F5DF83B03398129B4AC01DFE5971662B46130F3F5424AF93CF0A8C02A74CCF",
	})

	if msgs := readWSMessages(t, client.send); len(msgs) != 0 {
		t.Fatalf("expected custom app ID to be accepted, got %+v", msgs)
	}
	if trunkMgr.pushContact == nil || trunkMgr.pushContact.PNAppID != "th.or.ttrs.video.staging" {
		t.Fatalf("expected custom app ID to be persisted, got %+v", trunkMgr.pushContact)
	}

	srv.handleWSTrunkPushToken(client, WSMessage{
		Type:    "trunk_push_token",
		TrunkID: 42,
		PNAppID: config.DefaultTrunkPNAppID,
		PNType:  "apple",
		PNToken: "D6F5DF83B03398129B4AC01DFE5971662B46130F3F5424AF93CF0A8C02A74CCF",
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "error" || !strings.Contains(msgs[0].Error, "th.or.ttrs.video.staging") {
		t.Fatalf("expected old app ID to be rejected with custom app ID in error, got %+v", msgs)
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

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{
		send:       make(chan []byte, 8),
		authClaims: &auth.VerifiedClaims{Subject: "user-1"},
	}

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
	if client.resolvedTrunkID != 42 {
		t.Fatalf("expected client.resolvedTrunkID=42 for trunk_resolved by trunkPublicId, got %d", client.resolvedTrunkID)
	}
	if trunkMgr.lookupCount == 0 {
		t.Fatalf("expected GetTrunkIDByPublicID to be used")
	}
	if trunkMgr.notifyCallCount != 1 {
		t.Fatalf("expected notify_user_id update once, got %d", trunkMgr.notifyCallCount)
	}
	if trunkMgr.notifyTrunkID != 42 {
		t.Fatalf("expected notify_user_id update for resolved trunk 42, got %d", trunkMgr.notifyTrunkID)
	}
	if trunkMgr.notifyUserID == nil || *trunkMgr.notifyUserID != "user-1" {
		t.Fatalf("expected notify_user_id user-1, got %#v", trunkMgr.notifyUserID)
	}
}

func TestHandleWSTrunkResolve_ByTrunkPublicID_NormalizesUppercaseValue(t *testing.T) {
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

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSTrunkResolve(client, WSMessage{
		Type:          "trunk_resolve",
		SessionID:     "s1",
		TrunkPublicID: "8F6F6D70-2B5A-4FE7-A0D5-9D0AF0E90D3A",
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "trunk_resolved" {
		t.Fatalf("expected trunk_resolved, got %+v", msgs)
	}
	if msgs[0].TrunkPublicID != "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a" {
		t.Fatalf("expected normalized trunkPublicId in response, got %q", msgs[0].TrunkPublicID)
	}
	if !client.trunkResolved {
		t.Fatalf("expected client.trunkResolved=true for normalized trunkPublicId")
	}
	if client.resolvedTrunkID != 42 {
		t.Fatalf("expected client.resolvedTrunkID=42 for normalized trunkPublicId, got %d", client.resolvedTrunkID)
	}
}

func TestHandleWSTrunkResolve_LogsMobileResolvedSuccessByTrunkID(t *testing.T) {
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
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{
		send: make(chan []byte, 8),
		authClaims: &auth.VerifiedClaims{
			Subject: "user-1",
			Realm:   auth.TokenRealmUser,
		},
	}

	logs := captureStandardLogs(t, func() {
		srv.handleWSTrunkResolve(client, WSMessage{
			Type:      "trunk_resolve",
			SessionID: "s1",
			TrunkID:   42,
		})
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "trunk_resolved" {
		t.Fatalf("expected trunk_resolved, got %+v", msgs)
	}
	if !strings.Contains(logs, "[WS Trunk Resolve] mobile_resolved") {
		t.Fatalf("expected mobile resolved log, got %q", logs)
	}
	if !strings.Contains(logs, "sessionID=s1") || !strings.Contains(logs, "trunkID=42") {
		t.Fatalf("expected sessionID/trunkID fields in log, got %q", logs)
	}
	if !strings.Contains(logs, "authSub=user-1") || !strings.Contains(logs, "resolveBy=trunkId") {
		t.Fatalf("expected authSub/resolveBy in log, got %q", logs)
	}
}

func TestHandleWSTrunkResolve_LogsMobileResolvedSuccessByTrunkPublicID(t *testing.T) {
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

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{
		send: make(chan []byte, 8),
		authClaims: &auth.VerifiedClaims{
			Subject: "user-1",
			Realm:   auth.TokenRealmUser,
		},
	}

	logs := captureStandardLogs(t, func() {
		srv.handleWSTrunkResolve(client, WSMessage{
			Type:          "trunk_resolve",
			SessionID:     "s1",
			TrunkPublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a",
		})
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "trunk_resolved" {
		t.Fatalf("expected trunk_resolved, got %+v", msgs)
	}
	if !strings.Contains(logs, "resolveBy=trunkPublicId") {
		t.Fatalf("expected trunkPublicId resolveBy in log, got %q", logs)
	}
}

func TestHandleWSTrunkResolve_DoesNotLogMobileResolvedForNonUserRealm(t *testing.T) {
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
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{
		send: make(chan []byte, 8),
		authClaims: &auth.VerifiedClaims{
			Subject: "employee-1",
			Realm:   auth.TokenRealmEmployee,
		},
	}

	logs := captureStandardLogs(t, func() {
		srv.handleWSTrunkResolve(client, WSMessage{
			Type:      "trunk_resolve",
			SessionID: "s1",
			TrunkID:   42,
		})
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "trunk_resolved" {
		t.Fatalf("expected trunk_resolved, got %+v", msgs)
	}
	if strings.Contains(logs, "mobile_resolved") {
		t.Fatalf("did not expect mobile resolved log for non-user realm, got %q", logs)
	}
}

func TestHandleWSTrunkResolve_DoesNotLogMobileResolvedForNonSuccessOutcomes(t *testing.T) {
	t.Run("trunk_not_found", func(t *testing.T) {
		trunkMgr := &stubResolveTrunkManager{
			byID: map[int64]*sip.Trunk{},
		}
		srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
		client := &WSClient{
			send: make(chan []byte, 8),
			authClaims: &auth.VerifiedClaims{
				Subject: "user-1",
				Realm:   auth.TokenRealmUser,
			},
		}

		logs := captureStandardLogs(t, func() {
			srv.handleWSTrunkResolve(client, WSMessage{
				Type:      "trunk_resolve",
				SessionID: "s1",
				TrunkID:   999,
			})
		})
		if strings.Contains(logs, "mobile_resolved") {
			t.Fatalf("did not expect mobile resolved log for trunk_not_found, got %q", logs)
		}
	})

	t.Run("trunk_not_ready", func(t *testing.T) {
		owner := "gw-1"
		past := time.Now().Add(-1 * time.Minute)
		trunkMgr := &stubResolveTrunkManager{
			byID: map[int64]*sip.Trunk{
				42: {ID: 42, LeaseOwner: &owner, LeaseUntil: &past},
			},
		}
		srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
		client := &WSClient{
			send: make(chan []byte, 8),
			authClaims: &auth.VerifiedClaims{
				Subject: "user-1",
				Realm:   auth.TokenRealmUser,
			},
		}

		logs := captureStandardLogs(t, func() {
			srv.handleWSTrunkResolve(client, WSMessage{
				Type:      "trunk_resolve",
				SessionID: "s1",
				TrunkID:   42,
			})
		})
		if strings.Contains(logs, "mobile_resolved") {
			t.Fatalf("did not expect mobile resolved log for trunk_not_ready, got %q", logs)
		}
	})

	t.Run("trunk_redirect", func(t *testing.T) {
		owner := "gw-2"
		future := time.Now().Add(2 * time.Minute)
		trunkMgr := &stubResolveTrunkManager{
			byID: map[int64]*sip.Trunk{
				42: {ID: 42, LeaseOwner: &owner, LeaseUntil: &future},
			},
		}
		store := &stubResolveStore{
			lookupWSURL: "wss://gw-2.example.com/ws",
			lookupFound: true,
		}
		srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, store)
		client := &WSClient{
			send: make(chan []byte, 8),
			authClaims: &auth.VerifiedClaims{
				Subject: "user-1",
				Realm:   auth.TokenRealmUser,
			},
		}

		logs := captureStandardLogs(t, func() {
			srv.handleWSTrunkResolve(client, WSMessage{
				Type:      "trunk_resolve",
				SessionID: "s1",
				TrunkID:   42,
			})
		})
		if strings.Contains(logs, "mobile_resolved") {
			t.Fatalf("did not expect mobile resolved log for trunk_redirect, got %q", logs)
		}
	})
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

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
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
	if client.resolvedTrunkID != 0 {
		t.Fatalf("expected client.resolvedTrunkID=0 for trunk_not_ready by trunkId, got %d", client.resolvedTrunkID)
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

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, store)
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
	if client.resolvedTrunkID != 0 {
		t.Fatalf("expected client.resolvedTrunkID=0 for trunk_redirect by trunkId, got %d", client.resolvedTrunkID)
	}
}

func TestHandleWSTrunkResolve_NotifyUserIDAuditLog_SuccessByTrunkID(t *testing.T) {
	owner := "gw-1"
	future := time.Now().Add(2 * time.Minute)
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {ID: 42, PublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a", Username: "1001", LeaseOwner: &owner, LeaseUntil: &future},
		},
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{
		send: make(chan []byte, 8),
		authClaims: &auth.VerifiedClaims{
			Subject:           "user-sub-1",
			PreferredUsername: "alice",
		},
	}

	logs := captureStandardLogs(t, func() {
		srv.handleWSTrunkResolve(client, WSMessage{Type: "trunk_resolve", SessionID: "s1", TrunkID: 42})
	})
	if !strings.Contains(logs, "[Trunk NotifyUserID] updated") {
		t.Fatalf("expected notify_user_id updated log, got %q", logs)
	}
	if !strings.Contains(logs, "preferredUsername=alice") || !strings.Contains(logs, "sipUsername=1001") || !strings.Contains(logs, "authSub=user-sub-1") {
		t.Fatalf("expected preferredUsername/sipUsername/authSub in log, got %q", logs)
	}
}

func TestHandleWSTrunkResolve_NotifyUserIDAuditLog_SuccessByTrunkPublicID(t *testing.T) {
	owner := "gw-1"
	future := time.Now().Add(2 * time.Minute)
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {ID: 42, PublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a", Username: "1002", LeaseOwner: &owner, LeaseUntil: &future},
		},
		byPublicID: map[string]int64{"8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a": 42},
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{
		send: make(chan []byte, 8),
		authClaims: &auth.VerifiedClaims{
			Subject:           "user-sub-2",
			PreferredUsername: "bob",
		},
	}

	logs := captureStandardLogs(t, func() {
		srv.handleWSTrunkResolve(client, WSMessage{
			Type:          "trunk_resolve",
			SessionID:     "s2",
			TrunkPublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a",
		})
	})
	if !strings.Contains(logs, "preferredUsername=bob") || !strings.Contains(logs, "sipUsername=1002") || !strings.Contains(logs, "authSub=user-sub-2") {
		t.Fatalf("expected preferredUsername/sipUsername/authSub in log, got %q", logs)
	}
}

func TestHandleWSTrunkResolve_NotifyUserIDAuditLog_SuccessByCredentialsUsesDBUsername(t *testing.T) {
	owner := "gw-1"
	future := time.Now().Add(2 * time.Minute)
	store := &stubResolveStore{
		resolveTrunkID:    42,
		resolveLeaseOwner: &owner,
		resolveLeaseUntil: &future,
		resolveFound:      true,
	}
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {ID: 42, PublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a", Username: "canonical-1003", LeaseOwner: &owner, LeaseUntil: &future},
		},
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, store)
	client := &WSClient{
		send: make(chan []byte, 8),
		authClaims: &auth.VerifiedClaims{
			Subject:           "user-sub-3",
			PreferredUsername: "charlie",
		},
	}

	logs := captureStandardLogs(t, func() {
		srv.handleWSTrunkResolve(client, WSMessage{
			Type:        "trunk_resolve",
			SessionID:   "s3",
			SIPDomain:   "sip.example.com",
			SIPUsername: "payload-1003",
			SIPPassword: "secret",
			SIPPort:     5060,
		})
	})
	if !strings.Contains(logs, "preferredUsername=charlie") || !strings.Contains(logs, "sipUsername=canonical-1003") || !strings.Contains(logs, "authSub=user-sub-3") {
		t.Fatalf("expected preferredUsername/sipUsername/authSub in log, got %q", logs)
	}
}

func TestHandleWSTrunkResolve_NotifyUserIDAuditLog_NoClaimsNoLog(t *testing.T) {
	owner := "gw-1"
	future := time.Now().Add(2 * time.Minute)
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {ID: 42, Username: "1001", LeaseOwner: &owner, LeaseUntil: &future},
		},
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{send: make(chan []byte, 8)}

	logs := captureStandardLogs(t, func() {
		srv.handleWSTrunkResolve(client, WSMessage{Type: "trunk_resolve", SessionID: "s4", TrunkID: 42})
	})
	if strings.Contains(logs, "[Trunk NotifyUserID]") {
		t.Fatalf("did not expect notify_user_id audit log without auth claims, got %q", logs)
	}
}

func TestHandleWSTrunkResolve_NotifyUserIDAuditLog_ErrorIncludesContext(t *testing.T) {
	owner := "gw-1"
	future := time.Now().Add(2 * time.Minute)
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {ID: 42, PublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a", Username: "1004", LeaseOwner: &owner, LeaseUntil: &future},
		},
		notifyErr: errors.New("write failed"),
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, &stubResolveStore{})
	client := &WSClient{
		send: make(chan []byte, 8),
		authClaims: &auth.VerifiedClaims{
			Subject:           "user-sub-4",
			PreferredUsername: "dave",
		},
	}

	logs := captureStandardLogs(t, func() {
		srv.handleWSTrunkResolve(client, WSMessage{Type: "trunk_resolve", SessionID: "s5", TrunkID: 42})
	})
	if !strings.Contains(logs, "[Trunk NotifyUserID] update_failed") {
		t.Fatalf("expected update_failed log, got %q", logs)
	}
	if !strings.Contains(logs, "preferredUsername=dave") || !strings.Contains(logs, "sipUsername=1004") || !strings.Contains(logs, "authSub=user-sub-4") || !strings.Contains(logs, "error=write failed") {
		t.Fatalf("expected failure context in log, got %q", logs)
	}
}

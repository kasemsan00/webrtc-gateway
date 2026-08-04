package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"k2-gateway/internal/config"
	"k2-gateway/internal/session"
	"k2-gateway/internal/sip"
)

type agentTrunkManagerStub struct {
	trunk           *sip.Trunk
	upsertErr       error
	upsertPayload   sip.AgentTrunkPayload
	registerErr     error
	registerCount   int
	registerID      int64
	unregisterErr   error
	unregisterCount int
	unregisterID    int64
}

func (s *agentTrunkManagerStub) GetTrunkByID(id int64) (interface{}, bool) {
	if s.trunk != nil && s.trunk.ID == id {
		return s.trunk, true
	}
	return nil, false
}
func (s *agentTrunkManagerStub) GetTrunkByPublicID(string) (interface{}, bool) { return nil, false }
func (s *agentTrunkManagerStub) GetTrunkIDByPublicID(string) (int64, bool)    { return 0, false }
func (s *agentTrunkManagerStub) GetDefaultTrunk() (interface{}, bool)         { return nil, false }
func (s *agentTrunkManagerStub) RefreshTrunks() error                         { return nil }
func (s *agentTrunkManagerStub) CreateTrunk(context.Context, sip.CreateTrunkPayload) (*sip.Trunk, error) {
	return nil, errors.New("not implemented")
}
func (s *agentTrunkManagerStub) UpdateTrunk(context.Context, int64, sip.TrunkUpdatePatch) (*sip.Trunk, error) {
	return nil, errors.New("not implemented")
}
func (s *agentTrunkManagerStub) RegisterTrunk(trunkID int64, _ bool) error {
	s.registerCount++
	s.registerID = trunkID
	return s.registerErr
}
func (s *agentTrunkManagerStub) UnregisterTrunk(trunkID int64, _ bool) error {
	s.unregisterCount++
	s.unregisterID = trunkID
	return s.unregisterErr
}
func (s *agentTrunkManagerStub) ListTrunks(context.Context, sip.TrunkListParams) (*sip.TrunkListResult, error) {
	return &sip.TrunkListResult{}, nil
}
func (s *agentTrunkManagerStub) GetTrunkByIDFromDB(_ context.Context, trunkID int64) (*sip.Trunk, error) {
	if s.trunk != nil && s.trunk.ID == trunkID {
		return s.trunk, nil
	}
	return nil, errors.New("not found")
}
func (s *agentTrunkManagerStub) ListOwnedTrunks() []*sip.Trunk { return nil }
func (s *agentTrunkManagerStub) SetTrunkInUseBy(context.Context, int64, *string) error {
	return nil
}
func (s *agentTrunkManagerStub) FindTrunkByInUseBy(context.Context, string) (*sip.Trunk, error) {
	return nil, nil
}
func (s *agentTrunkManagerStub) SetTrunkNotifyUserID(context.Context, int64, *string) error {
	return nil
}
func (s *agentTrunkManagerStub) SetTrunkNotifyUserIDAndPlatform(context.Context, int64, *string, *string) error {
	return nil
}
func (s *agentTrunkManagerStub) SetTrunkPushContact(context.Context, int64, sip.TrunkPushContact) (bool, error) {
	return true, nil
}
func (s *agentTrunkManagerStub) UpsertAgentTrunk(_ context.Context, payload sip.AgentTrunkPayload) (*sip.Trunk, error) {
	s.upsertPayload = payload
	if s.upsertErr != nil {
		return nil, s.upsertErr
	}
	if s.trunk == nil {
		s.trunk = &sip.Trunk{
			ID:       99,
			PublicID: "agent-public-99",
			Name:     "sipclient-agent-1001@sip.example.com:5060",
			Domain:   strings.ToLower(strings.TrimSpace(payload.Domain)),
			Username: strings.TrimSpace(payload.Username),
			Password: strings.TrimSpace(payload.Password),
			Port:     5060,
			Enabled:  true,
		}
	}
	return s.trunk, nil
}

func newAgentTestServer(t *testing.T, tm *agentTrunkManagerStub) *Server {
	t.Helper()
	mgr := session.NewManager(&config.Config{})
	return &Server{
		sessionMgr:         mgr,
		trunkManager:       tm,
		config:             config.APIConfig{EnableAgentWS: true},
		wsClients:          make(map[string]*WSClient),
		wsConnections:      make(map[*WSClient]struct{}),
		agentTrunkBindings: make(map[int64]map[*WSClient]struct{}),
		wsClientStreams:    make(map[int]chan []byte),
	}
}

func newAgentWSClient() *WSClient {
	return &WSClient{
		clientID:     "agent-client-1",
		send:         make(chan []byte, 8),
		availability: clientAvailabilityIdle,
		callState:    string(session.StateNew),
		ConnectedAt:  time.Now(),
		agentOnly:    true,
	}
}

func readAgentWSMessages(t *testing.T, client *WSClient) []WSMessage {
	t.Helper()
	var msgs []WSMessage
	for {
		select {
		case raw := <-client.send:
			var msg WSMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}

func TestAgentRegisterSuccessDoesNotEchoPassword(t *testing.T) {
	tm := &agentTrunkManagerStub{}
	srv := newAgentTestServer(t, tm)
	client := newAgentWSClient()

	srv.handleWSAgentRegister(client, WSMessage{
		Type:        "agent_register",
		SIPDomain:   "sip.example.com",
		SIPUsername: "1001",
		SIPPassword: "super-secret",
	})

	msgs := readAgentWSMessages(t, client)
	if len(msgs) != 1 || msgs[0].Type != "trunk_resolved" {
		t.Fatalf("expected trunk_resolved, got %+v", msgs)
	}
	if msgs[0].SIPPassword != "" {
		t.Fatalf("trunk_resolved must not echo password")
	}
	if msgs[0].TrunkID != 99 || msgs[0].TrunkPublicID != "agent-public-99" {
		t.Fatalf("unexpected trunk_resolved %+v", msgs[0])
	}
	if !client.trunkResolved || client.resolvedTrunkID != 99 {
		t.Fatalf("expected client bound to trunk 99")
	}
	if tm.registerCount != 1 {
		t.Fatalf("expected first bind to REGISTER once, got %d", tm.registerCount)
	}
	if tm.upsertPayload.Password != "super-secret" {
		t.Fatalf("expected upsert to receive password")
	}
}

func TestAgentRegisterFailureLeavesClientUnresolved(t *testing.T) {
	tm := &agentTrunkManagerStub{upsertErr: errors.New("db down")}
	srv := newAgentTestServer(t, tm)
	client := newAgentWSClient()

	srv.handleWSAgentRegister(client, WSMessage{
		Type:        "agent_register",
		SIPDomain:   "sip.example.com",
		SIPUsername: "1001",
		SIPPassword: "super-secret",
	})

	msgs := readAgentWSMessages(t, client)
	if len(msgs) != 1 || msgs[0].Type != "error" {
		t.Fatalf("expected error, got %+v", msgs)
	}
	if strings.Contains(msgs[0].Error, "super-secret") {
		t.Fatalf("error must not leak password: %q", msgs[0].Error)
	}
	if client.trunkResolved || client.resolvedTrunkID != 0 {
		t.Fatalf("client must remain unresolved on failure")
	}
	if tm.registerCount != 0 {
		t.Fatalf("REGISTER must not run after upsert failure")
	}
}

func TestAgentRegisterSIPRegisterFailureRollsBackBind(t *testing.T) {
	tm := &agentTrunkManagerStub{registerErr: errors.New("register failed")}
	srv := newAgentTestServer(t, tm)
	client := newAgentWSClient()

	srv.handleWSAgentRegister(client, WSMessage{
		Type:        "agent_register",
		SIPDomain:   "sip.example.com",
		SIPUsername: "1001",
		SIPPassword: "super-secret",
	})

	msgs := readAgentWSMessages(t, client)
	if len(msgs) != 1 || msgs[0].Type != "error" {
		t.Fatalf("expected error, got %+v", msgs)
	}
	if client.trunkResolved || client.resolvedTrunkID != 0 {
		t.Fatalf("client must roll back bind on REGISTER failure")
	}
	if srv.agentTrunkRefCount(99) != 0 {
		t.Fatalf("expected refcount 0 after rollback")
	}
}

func TestAgentTwoClientsShareRegisterAndLastDisconnectUnregisters(t *testing.T) {
	tm := &agentTrunkManagerStub{}
	srv := newAgentTestServer(t, tm)
	a := newAgentWSClient()
	a.clientID = "a"
	b := newAgentWSClient()
	b.clientID = "b"

	msg := WSMessage{
		Type:        "agent_register",
		SIPDomain:   "sip.example.com",
		SIPUsername: "1001",
		SIPPassword: "super-secret",
	}
	srv.handleWSAgentRegister(a, msg)
	_ = readAgentWSMessages(t, a)
	srv.handleWSAgentRegister(b, msg)
	_ = readAgentWSMessages(t, b)

	if tm.registerCount != 1 {
		t.Fatalf("expected single REGISTER for two clients, got %d", tm.registerCount)
	}
	if srv.agentTrunkRefCount(99) != 2 {
		t.Fatalf("expected refcount 2, got %d", srv.agentTrunkRefCount(99))
	}

	srv.cleanupAgentPresence(a)
	if tm.unregisterCount != 0 {
		t.Fatalf("non-last disconnect must keep REGISTER")
	}
	if srv.agentTrunkRefCount(99) != 1 {
		t.Fatalf("expected refcount 1 after first disconnect")
	}

	srv.cleanupAgentPresence(b)
	if tm.unregisterCount != 1 || tm.unregisterID != 99 {
		t.Fatalf("last disconnect must unregister, got count=%d id=%d", tm.unregisterCount, tm.unregisterID)
	}
	if srv.agentTrunkRefCount(99) != 0 {
		t.Fatalf("expected refcount 0 after last disconnect")
	}
}

func TestAgentLastDisconnectHangsUpActiveCall(t *testing.T) {
	tm := &agentTrunkManagerStub{}
	sipMaker := &incomingTestSIPCallMaker{}
	srv := newAgentTestServer(t, tm)
	srv.sipMaker = sipMaker
	client := newAgentWSClient()

	srv.handleWSAgentRegister(client, WSMessage{
		Type:        "agent_register",
		SIPDomain:   "sip.example.com",
		SIPUsername: "1001",
		SIPPassword: "super-secret",
	})
	_ = readAgentWSMessages(t, client)

	sess, err := srv.sessionMgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sess.SetSIPAuthContext("trunk", "", 99, "sip.example.com", "1001", "super-secret", 5060)
	sess.UpdateState(session.StateActive)
	client.sessionID = sess.ID

	srv.cleanupAgentPresence(client)

	if sipMaker.hangupCount != 1 {
		t.Fatalf("expected hangup on last disconnect during call, got %d", sipMaker.hangupCount)
	}
	if tm.unregisterCount != 1 {
		t.Fatalf("expected unregister after hangup, got %d", tm.unregisterCount)
	}
	if _, ok := srv.sessionMgr.GetSession(sess.ID); ok {
		t.Fatalf("session should be deleted")
	}
}

func TestAgentAllowlistRejectsTrunkPushToken(t *testing.T) {
	srv := newAgentTestServer(t, &agentTrunkManagerStub{})
	client := newAgentWSClient()
	ok, reason := srv.allowAgentWSMessage(client, WSMessage{Type: "trunk_push_token"})
	if ok {
		t.Fatalf("trunk_push_token must be rejected on agent WS")
	}
	if reason == "" {
		t.Fatalf("expected rejection reason")
	}
}

func TestNormalizeDevicePlatformStillRejectsAgentValues(t *testing.T) {
	if _, ok := normalizeDevicePlatform("agent"); ok {
		t.Fatalf("agent must not be accepted as mobile devicePlatform")
	}
	if _, ok := normalizeDevicePlatform("pc"); ok {
		t.Fatalf("pc must not be accepted as mobile devicePlatform")
	}
	if v, ok := normalizeDevicePlatform("android"); !ok || v != "android" {
		t.Fatalf("android must remain valid")
	}
	if v, ok := normalizeDevicePlatform("ios"); !ok || v != "ios" {
		t.Fatalf("ios must remain valid")
	}
}

func TestMobileDisconnectDoesNotTriggerAgentUnregister(t *testing.T) {
	tm := &agentTrunkManagerStub{
		trunk: &sip.Trunk{ID: 42, PublicID: "mobile-42", Enabled: true},
	}
	srv := newAgentTestServer(t, tm)
	mobile := &WSClient{
		clientID:        "mobile-1",
		send:            make(chan []byte, 2),
		trunkResolved:   true,
		resolvedTrunkID: 42,
		agentOnly:       false,
	}
	srv.cleanupAgentPresence(mobile)
	if tm.unregisterCount != 0 {
		t.Fatalf("mobile disconnect must not unregister via agent cleanup")
	}
}

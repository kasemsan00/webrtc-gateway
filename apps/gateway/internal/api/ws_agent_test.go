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
func (s *agentTrunkManagerStub) GetTrunkIDByPublicID(string) (int64, bool)     { return 0, false }
func (s *agentTrunkManagerStub) GetDefaultTrunk() (interface{}, bool)          { return nil, false }
func (s *agentTrunkManagerStub) RefreshTrunks() error                          { return nil }
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
		clientID:        "agent-client-1",
		send:            make(chan []byte, 8),
		ownedSessionIDs: make(map[string]struct{}),
		pendingIncoming: make(map[string]struct{}),
		availability:    clientAvailabilityIdle,
		callState:       string(session.StateNew),
		ConnectedAt:     time.Now(),
		agentOnly:       true,
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
		MultiCall:   true,
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
	if !client.multiCall {
		t.Fatalf("expected explicit multiCall capability to be retained")
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

func TestDetachWSClientRemovesClosedAgentFromRoutingIndexes(t *testing.T) {
	tm := &agentTrunkManagerStub{}
	srv := newAgentTestServer(t, tm)
	client := newAgentWSClient()
	client.clientID = "closed-agent"
	client.trunkResolved = true
	client.resolvedTrunkID = 99
	srv.wsConnections[client] = struct{}{}
	srv.agentTrunkBindings[99] = map[*WSClient]struct{}{client: {}}
	srv.wsClients["call-1"] = client
	client.sessionID = "call-1"
	client.ownedSessionIDs["call-1"] = struct{}{}

	srv.detachWSClient(client)

	if _, ok := srv.wsConnections[client]; ok {
		t.Fatal("closed agent must be removed from wsConnections")
	}
	if _, ok := srv.wsClients["call-1"]; ok {
		t.Fatal("closed agent session route must be removed")
	}
	if srv.agentTrunkRefCount(99) != 1 {
		t.Fatal("detach must not mutate trunk binding before presence cleanup")
	}

	srv.cleanupAgentPresence(client)
	if srv.agentTrunkRefCount(99) != 0 {
		t.Fatal("presence cleanup must remove the agent binding")
	}
	if tm.unregisterCount != 1 || tm.unregisterID != 99 {
		t.Fatalf("last closed agent must unregister trunk, got count=%d id=%d", tm.unregisterCount, tm.unregisterID)
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

func TestAgentClientOwnsMultipleSessions(t *testing.T) {
	srv := newAgentTestServer(t, &agentTrunkManagerStub{})
	client := newAgentWSClient()
	first, err := srv.sessionMgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession first: %v", err)
	}
	second, err := srv.sessionMgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession second: %v", err)
	}

	srv.bindClientSession(client, first.ID)
	srv.bindClientSession(client, second.ID)

	if !srv.clientOwnsSession(client, first.ID) || !srv.clientOwnsSession(client, second.ID) {
		t.Fatalf("expected agent to own both sessions")
	}
	if got := srv.activeOwnedSessionCount(client); got != 2 {
		t.Fatalf("expected 2 active owned sessions, got %d", got)
	}
	if srv.wsClients[first.ID] != client || srv.wsClients[second.ID] != client {
		t.Fatalf("expected both session routes to point to the same agent connection")
	}
}

func TestAgentDisconnectEndsEveryOwnedSession(t *testing.T) {
	tm := &agentTrunkManagerStub{}
	sipMaker := &incomingTestSIPCallMaker{}
	srv := newAgentTestServer(t, tm)
	srv.sipMaker = sipMaker
	client := newAgentWSClient()
	client.trunkResolved = true
	client.resolvedTrunkID = 99
	srv.agentTrunkBindings[99] = map[*WSClient]struct{}{client: {}}

	for i := 0; i < 2; i++ {
		sess, err := srv.sessionMgr.CreateSession(config.TURNConfig{})
		if err != nil {
			t.Fatalf("CreateSession %d: %v", i, err)
		}
		sess.SetSIPAuthContext("trunk", "", 99, "sip.example.com", "1001", "secret", 5060)
		sess.UpdateState(session.StateActive)
		srv.bindClientSession(client, sess.ID)
	}

	srv.cleanupAgentPresence(client)

	if sipMaker.hangupCount != 2 {
		t.Fatalf("expected both calls to hang up, got %d", sipMaker.hangupCount)
	}
	if got := len(srv.sessionMgr.ListSessions()); got != 0 {
		t.Fatalf("expected every owned session deleted, got %d", got)
	}
	if got := len(client.ownedSessionIDs); got != 0 {
		t.Fatalf("expected ownership cleared, got %d", got)
	}
	if tm.unregisterCount != 1 {
		t.Fatalf("expected last agent disconnect to unregister once, got %d", tm.unregisterCount)
	}
}

func TestBusyMultiCallAgentReceivesWaitingIncoming(t *testing.T) {
	sipMaker := &incomingTestSIPCallMaker{}
	srv := newAgentTestServer(t, &agentTrunkManagerStub{})
	srv.sipMaker = sipMaker
	client := newAgentWSClient()
	client.trunkResolved = true
	client.resolvedTrunkID = 99
	client.multiCall = true
	client.availability = clientAvailabilityBusy
	client.callState = string(session.StateActive)
	srv.wsConnections[client] = struct{}{}

	incoming, err := srv.sessionMgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	incoming.SetState(session.StateIncoming)
	srv.NotifyIncomingCall(incoming.ID, "sip:2002@example.com", "sip:1001@example.com", 99)

	msgs := readAgentWSMessages(t, client)
	if len(msgs) != 1 || msgs[0].Type != "incoming" || msgs[0].SessionID != incoming.ID {
		t.Fatalf("expected waiting incoming notification, got %+v", msgs)
	}
	if !srv.clientHasPendingIncoming(client, incoming.ID) {
		t.Fatalf("expected incoming session ownership to be presented")
	}
	if sipMaker.rejectCount != 0 {
		t.Fatalf("multi-call agent must not busy-reject waiting call")
	}
}

func TestBusyLegacyAgentStillRejectsWaitingIncoming(t *testing.T) {
	sipMaker := &incomingTestSIPCallMaker{}
	srv := newAgentTestServer(t, &agentTrunkManagerStub{})
	srv.sipMaker = sipMaker
	client := newAgentWSClient()
	client.trunkResolved = true
	client.resolvedTrunkID = 99
	client.availability = clientAvailabilityBusy
	client.callState = string(session.StateActive)
	srv.wsConnections[client] = struct{}{}

	incoming, err := srv.sessionMgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	incoming.SetState(session.StateIncoming)
	srv.NotifyIncomingCall(incoming.ID, "sip:2002@example.com", "sip:1001@example.com", 99)

	if got := len(readAgentWSMessages(t, client)); got != 0 {
		t.Fatalf("legacy busy agent must not receive incoming notification")
	}
	if sipMaker.rejectCount != 1 || sipMaker.lastReject != "busy" {
		t.Fatalf("expected one busy rejection, count=%d reason=%q", sipMaker.rejectCount, sipMaker.lastReject)
	}
}

func TestAgentAcceptKeepsPresentedIncomingSessionID(t *testing.T) {
	sipMaker := &incomingTestSIPCallMaker{}
	srv := newAgentTestServer(t, &agentTrunkManagerStub{})
	srv.sipMaker = sipMaker
	client := newAgentWSClient()
	client.multiCall = true

	incoming, err := srv.sessionMgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	incoming.SetState(session.StateIncoming)
	incoming.SetCallInfo("inbound", "sip:2002@example.com", "sip:1001@example.com", "sip-call-stable")
	srv.markPendingIncoming(client, incoming.ID)
	// A multi-call client prepares the incoming session in place with
	// offer.sessionId before it sends accept.
	srv.bindClientSession(client, incoming.ID)

	srv.handleWSAccept(client, WSMessage{Type: "accept", SessionID: incoming.ID})

	if sipMaker.acceptCount != 1 {
		t.Fatalf("expected AcceptCall once, got %d", sipMaker.acceptCount)
	}
	if client.sessionID != incoming.ID || !srv.clientOwnsSession(client, incoming.ID) {
		t.Fatalf("expected stable accepted session ID %s", incoming.ID)
	}
	if got := len(srv.sessionMgr.ListSessions()); got != 1 {
		t.Fatalf("expected no replacement session, got %d sessions", got)
	}
	msgs := readAgentWSMessages(t, client)
	if len(msgs) != 1 || msgs[0].Type != "state" || msgs[0].SessionID != incoming.ID || msgs[0].State != "active" {
		t.Fatalf("expected active state for stable session, got %+v", msgs)
	}
}

type agentHoldSIPMaker struct {
	incomingTestSIPCallMaker
	holdCount int
	lastHeld  bool
	holdErr   error
}

func (s *agentHoldSIPMaker) SetHold(sess *session.Session, held bool) error {
	s.holdCount++
	s.lastHeld = held
	if s.holdErr != nil {
		return s.holdErr
	}
	sess.SetHeld(held)
	return nil
}

func TestAgentHoldUnholdAreOwnedIdempotentAndReportState(t *testing.T) {
	holdMaker := &agentHoldSIPMaker{}
	srv := newAgentTestServer(t, &agentTrunkManagerStub{})
	srv.sipMaker = holdMaker
	client := newAgentWSClient()
	sess, err := srv.sessionMgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sess.UpdateState(session.StateActive)
	sess.SetSIPDialogState("local", "remote", "<sip:2002@example.com>", "example.com", 5060, 1, nil)
	srv.bindClientSession(client, sess.ID)

	srv.handleWSMessage(client, []byte(`{"type":"hold","sessionId":"`+sess.ID+`"}`))
	srv.handleWSMessage(client, []byte(`{"type":"hold","sessionId":"`+sess.ID+`"}`))
	srv.handleWSMessage(client, []byte(`{"type":"unhold","sessionId":"`+sess.ID+`"}`))

	if holdMaker.holdCount != 2 || holdMaker.lastHeld {
		t.Fatalf("expected one hold and one unhold SIP update, count=%d lastHeld=%v", holdMaker.holdCount, holdMaker.lastHeld)
	}
	msgs := readAgentWSMessages(t, client)
	if len(msgs) != 3 {
		t.Fatalf("expected hold, idempotent hold, and unhold responses, got %+v", msgs)
	}
	if msgs[0].Held == nil || !*msgs[0].Held || msgs[1].Held == nil || !*msgs[1].Held || msgs[2].Held == nil || *msgs[2].Held {
		t.Fatalf("unexpected hold_state sequence: %+v", msgs)
	}
}

func TestAgentHoldFailureAndUnownedSessionReturnErrors(t *testing.T) {
	holdMaker := &agentHoldSIPMaker{holdErr: errors.New("re-INVITE rejected")}
	srv := newAgentTestServer(t, &agentTrunkManagerStub{})
	srv.sipMaker = holdMaker
	client := newAgentWSClient()
	owned, err := srv.sessionMgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession owned: %v", err)
	}
	owned.UpdateState(session.StateActive)
	srv.bindClientSession(client, owned.ID)
	unowned, err := srv.sessionMgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession unowned: %v", err)
	}
	unowned.UpdateState(session.StateActive)

	srv.handleWSMessage(client, []byte(`{"type":"hold","sessionId":"`+owned.ID+`"}`))
	srv.handleWSMessage(client, []byte(`{"type":"hold","sessionId":"`+unowned.ID+`"}`))

	if owned.IsHeld() {
		t.Fatalf("failed SIP hold must not change session hold state")
	}
	msgs := readAgentWSMessages(t, client)
	if len(msgs) != 2 || msgs[0].Type != "error" || msgs[1].Type != "error" {
		t.Fatalf("expected errors for failed and unowned hold, got %+v", msgs)
	}
	if !strings.Contains(msgs[0].Error, "re-INVITE rejected") {
		t.Fatalf("expected SIP hold failure detail, got %q", msgs[0].Error)
	}
}

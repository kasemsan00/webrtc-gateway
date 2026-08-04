package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"k2-gateway/internal/config"
	"k2-gateway/internal/push"
	"k2-gateway/internal/session"
	"k2-gateway/internal/sip"
)

type incomingTestSIPCallMaker struct {
	acceptCount int
	rejectCount int
	lastReject  string
	rejectErr   error
	hangupCount int
	cancelCount int
	lastHangup  *session.Session
}

func (s *incomingTestSIPCallMaker) MakeCall(destination, from string, sess *session.Session) error {
	return nil
}
func (s *incomingTestSIPCallMaker) Hangup(sess *session.Session) error {
	s.hangupCount++
	s.lastHangup = sess
	return nil
}
func (s *incomingTestSIPCallMaker) CancelPendingCall(sess *session.Session) error {
	s.cancelCount++
	return nil
}
func (s *incomingTestSIPCallMaker) SendDTMF(sess *session.Session, digits string) error { return nil }
func (s *incomingTestSIPCallMaker) AcceptCall(sess *session.Session) error {
	s.acceptCount++
	sess.SetSIPDialogState(
		"local-tag",
		"remote-tag",
		"<sip:00025@203.150.245.42:5060>",
		"203.151.21.121",
		5060,
		1,
		nil,
	)
	sess.UpdateState(session.StateActive)
	return nil
}
func (s *incomingTestSIPCallMaker) RejectCall(sess *session.Session, reason string) error {
	s.rejectCount++
	s.lastReject = reason
	return s.rejectErr
}
func (s *incomingTestSIPCallMaker) SendMessage(destination, from, body, contentType string) error {
	return nil
}
func (s *incomingTestSIPCallMaker) SendMessageToSession(sess *session.Session, body, contentType string) error {
	return nil
}
func (s *incomingTestSIPCallMaker) TriggerSwitchMessage(body, callerURI string) error {
	return nil
}

type incomingNotifyTestTrunkManager struct {
	trunkByID             map[int64]*sip.Trunk
	getTrunkByIDCalls     int
	getTrunkByDBCalls     int
	getTrunkByDBErr       error
	getTrunkByDBCompleted chan struct{}
}

func (s *incomingNotifyTestTrunkManager) signalDBLookupComplete() {
	if s.getTrunkByDBCompleted == nil {
		return
	}
	select {
	case s.getTrunkByDBCompleted <- struct{}{}:
	default:
	}
}

func (s *incomingNotifyTestTrunkManager) GetTrunkByID(id int64) (interface{}, bool) {
	s.getTrunkByIDCalls++
	trunk, ok := s.trunkByID[id]
	if !ok {
		return nil, false
	}
	return trunk, true
}

func (s *incomingNotifyTestTrunkManager) GetTrunkByPublicID(publicID string) (interface{}, bool) {
	return nil, false
}

func (s *incomingNotifyTestTrunkManager) GetTrunkIDByPublicID(publicID string) (int64, bool) {
	return 0, false
}

func (s *incomingNotifyTestTrunkManager) GetDefaultTrunk() (interface{}, bool) {
	return nil, false
}

func (s *incomingNotifyTestTrunkManager) RefreshTrunks() error {
	return nil
}

func (s *incomingNotifyTestTrunkManager) CreateTrunk(ctx context.Context, payload sip.CreateTrunkPayload) (*sip.Trunk, error) {
	return nil, errors.New("not implemented")
}

func (s *incomingNotifyTestTrunkManager) UpdateTrunk(ctx context.Context, trunkID int64, patch sip.TrunkUpdatePatch) (*sip.Trunk, error) {
	return nil, errors.New("not implemented")
}

func (s *incomingNotifyTestTrunkManager) RegisterTrunk(trunkID int64, force bool) error {
	return nil
}

func (s *incomingNotifyTestTrunkManager) UnregisterTrunk(trunkID int64, force bool) error {
	return nil
}

func (s *incomingNotifyTestTrunkManager) ListTrunks(ctx context.Context, params sip.TrunkListParams) (*sip.TrunkListResult, error) {
	return &sip.TrunkListResult{Items: []*sip.Trunk{}, Total: 0, Page: 1, PageSize: 10}, nil
}

func (s *incomingNotifyTestTrunkManager) GetTrunkByIDFromDB(ctx context.Context, trunkID int64) (*sip.Trunk, error) {
	s.getTrunkByDBCalls++
	defer s.signalDBLookupComplete()
	if s.getTrunkByDBErr != nil {
		return nil, s.getTrunkByDBErr
	}
	trunk, ok := s.trunkByID[trunkID]
	if !ok {
		return nil, nil
	}
	return trunk, nil
}

func (s *incomingNotifyTestTrunkManager) ListOwnedTrunks() []*sip.Trunk {
	return []*sip.Trunk{}
}

func (s *incomingNotifyTestTrunkManager) SetTrunkInUseBy(ctx context.Context, trunkID int64, username *string) error {
	return nil
}

func (s *incomingNotifyTestTrunkManager) FindTrunkByInUseBy(ctx context.Context, inUseBy string) (*sip.Trunk, error) {
	return nil, nil
}

func (s *incomingNotifyTestTrunkManager) SetTrunkNotifyUserID(ctx context.Context, trunkID int64, userID *string) error {
	return nil
}

func (s *incomingNotifyTestTrunkManager) SetTrunkNotifyUserIDAndPlatform(ctx context.Context, trunkID int64, userID *string, platform *string) error {
	return nil
}

func (s *incomingNotifyTestTrunkManager) SetTrunkPushContact(ctx context.Context, trunkID int64, contact sip.TrunkPushContact) (bool, error) {
	return true, nil
}

func (s *incomingNotifyTestTrunkManager) UpsertAgentTrunk(_ context.Context, _ sip.AgentTrunkPayload) (*sip.Trunk, error) {
	return nil, errors.New("not implemented")
}

func waitForDBLookups(t *testing.T, trunkMgr *incomingNotifyTestTrunkManager, want int) {
	t.Helper()
	for i := 0; i < want; i++ {
		select {
		case <-trunkMgr.getTrunkByDBCompleted:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("expected DB lookup %d/%d to complete", i+1, want)
		}
	}
}

func TestHandleWSAccept_FirstAcceptWins(t *testing.T) {
	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "1001", "1002", "sip-call-1")

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)

	webrtcSess1, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create first webrtc session: %v", err)
	}
	webrtcSess2, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create second webrtc session: %v", err)
	}
	client1 := &WSClient{sessionID: webrtcSess1.ID, send: make(chan []byte, 8)}
	client2 := &WSClient{sessionID: webrtcSess2.ID, send: make(chan []byte, 8)}

	srv.handleWSAccept(client1, WSMessage{
		Type:      "accept",
		SessionID: incomingSess.ID,
	})

	msgs1 := readWSMessages(t, client1.send)
	if len(msgs1) != 1 {
		t.Fatalf("expected 1 message for first accept, got %d", len(msgs1))
	}
	if msgs1[0].Type != "state" || msgs1[0].State != "active" {
		t.Fatalf("expected active state for first accept, got type=%s state=%s", msgs1[0].Type, msgs1[0].State)
	}

	srv.handleWSAccept(client2, WSMessage{
		Type:      "accept",
		SessionID: incomingSess.ID,
	})

	msgs2 := readWSMessages(t, client2.send)
	if len(msgs2) != 1 {
		t.Fatalf("expected 1 message for second accept, got %d", len(msgs2))
	}
	if msgs2[0].Type != "error" {
		t.Fatalf("expected stale second accept to receive error, got type=%s state=%s", msgs2[0].Type, msgs2[0].State)
	}

	if sipMaker.acceptCount != 1 {
		t.Fatalf("expected AcceptCall once, got %d", sipMaker.acceptCount)
	}
}

func TestHandleWSReject_DefaultReasonAndDeletesSession(t *testing.T) {
	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "1001", "1002", "sip-call-2")

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSReject(client, WSMessage{
		Type:      "reject",
		SessionID: incomingSess.ID,
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for reject, got %d", len(msgs))
	}
	if msgs[0].Type != "state" {
		t.Fatalf("expected state message, got %s", msgs[0].Type)
	}
	if msgs[0].State != string(session.StateEnded) {
		t.Fatalf("expected ended state, got %s", msgs[0].State)
	}
	if sipMaker.rejectCount != 1 {
		t.Fatalf("expected RejectCall once, got %d", sipMaker.rejectCount)
	}
	if sipMaker.lastReject != "busy" {
		t.Fatalf("expected default reject reason busy, got %q", sipMaker.lastReject)
	}
	if _, ok := mgr.GetSession(incomingSess.ID); ok {
		t.Fatalf("expected session %s to be deleted after reject", incomingSess.ID)
	}
}

func TestHandleWSReject_WithReasonSourceUnavailableEndsSessionOnce(t *testing.T) {
	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "1001", "1002", "sip-call-2-source")

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSReject(client, WSMessage{
		Type:         "reject",
		SessionID:    incomingSess.ID,
		Reason:       "unavailable",
		ReasonSource: "callkeep_create_incoming_connection_failed",
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for source-aware reject, got %d", len(msgs))
	}
	if msgs[0].Type != "state" || msgs[0].State != string(session.StateEnded) {
		t.Fatalf("expected ended state, got type=%s state=%s", msgs[0].Type, msgs[0].State)
	}
	if sipMaker.rejectCount != 1 {
		t.Fatalf("expected RejectCall once, got %d", sipMaker.rejectCount)
	}
	if sipMaker.lastReject != "unavailable" {
		t.Fatalf("expected unavailable reject reason, got %q", sipMaker.lastReject)
	}
}

func TestHandleWSReject_DuplicateRejectSendsSingleTerminalResponse(t *testing.T) {
	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "1001", "1002", "sip-call-reject-race")

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client1 := &WSClient{send: make(chan []byte, 8)}
	client2 := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSReject(client1, WSMessage{Type: "reject", SessionID: incomingSess.ID, Reason: "busy", ReasonSource: "in_app_decline"})
	srv.handleWSReject(client2, WSMessage{Type: "reject", SessionID: incomingSess.ID, Reason: "unavailable", ReasonSource: "callkeep_create_incoming_connection_failed"})

	if sipMaker.rejectCount != 1 {
		t.Fatalf("expected only one RejectCall for duplicate rejects, got %d", sipMaker.rejectCount)
	}
	if sipMaker.lastReject != "busy" {
		t.Fatalf("expected first reject reason to win, got %q", sipMaker.lastReject)
	}

	msgs1 := readWSMessages(t, client1.send)
	if len(msgs1) != 1 || msgs1[0].Type != "state" || msgs1[0].State != string(session.StateEnded) {
		t.Fatalf("expected first client ended state, got %+v", msgs1)
	}
	msgs2 := readWSMessages(t, client2.send)
	if len(msgs2) != 1 || msgs2[0].Type != "error" {
		t.Fatalf("expected second client to see error for missing session after winner cleanup, got %+v", msgs2)
	}
}

func TestHandleWSReject_BenignTransactionTerminatedIsTreatedAsEnded(t *testing.T) {
	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "1001", "1002", "sip-call-terminated")

	sipMaker := &incomingTestSIPCallMaker{
		rejectErr: errors.New("failed to send reject response: transaction terminated"),
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSReject(client, WSMessage{
		Type:      "reject",
		SessionID: incomingSess.ID,
		Reason:    "decline",
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for reject benign path, got %d", len(msgs))
	}
	if msgs[0].Type != "state" || msgs[0].State != string(session.StateEnded) {
		t.Fatalf("expected ended state for benign reject path, got type=%s state=%s", msgs[0].Type, msgs[0].State)
	}
	if _, ok := mgr.GetSession(incomingSess.ID); ok {
		t.Fatalf("expected session %s to be deleted after benign reject path", incomingSess.ID)
	}
}

func TestHandleWSReject_NonBenignErrorReturnsWSError(t *testing.T) {
	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "1001", "1002", "sip-call-fail")

	sipMaker := &incomingTestSIPCallMaker{
		rejectErr: errors.New("network timeout"),
	}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSReject(client, WSMessage{
		Type:      "reject",
		SessionID: incomingSess.ID,
		Reason:    "decline",
	})

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for reject non-benign path, got %d", len(msgs))
	}
	if msgs[0].Type != "error" {
		t.Fatalf("expected error response for non-benign reject path, got %s", msgs[0].Type)
	}
	if _, ok := mgr.GetSession(incomingSess.ID); !ok {
		t.Fatalf("expected session %s to remain when reject fails with non-benign error", incomingSess.ID)
	}
}

func TestNotifyIncomingCall_AllClientsBusyRejectsBusy(t *testing.T) {
	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "1001", "1002", "sip-call-busy")
	incomingSess.SetSIPAuthContext("trunk", "", 7, "sip.example.com", "1002", "secret", 5060)

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	busyClient := &WSClient{
		send:            make(chan []byte, 8),
		trunkResolved:   true,
		resolvedTrunkID: 7,
		availability:    clientAvailabilityBusy,
		callState:       "incall",
	}
	srv.wsConnections[busyClient] = struct{}{}

	srv.NotifyIncomingCall(incomingSess.ID, "1001", "1002", 7)

	if sipMaker.rejectCount != 1 || sipMaker.lastReject != "busy" {
		t.Fatalf("expected busy reject once, got count=%d reason=%q", sipMaker.rejectCount, sipMaker.lastReject)
	}
	if msgs := readWSMessages(t, busyClient.send); len(msgs) != 0 {
		t.Fatalf("expected no incoming sent to busy client, got %d messages", len(msgs))
	}
	if _, ok := mgr.GetSession(incomingSess.ID); ok {
		t.Fatalf("expected busy incoming session deleted")
	}
}

func TestNotifyIncomingCall_BusyClientWithPushTargetDoesNotDispatchPush(t *testing.T) {
	notifyUserID := "user-1"
	trunkMgr := &incomingNotifyTestTrunkManager{
		trunkByID: map[int64]*sip.Trunk{
			7: {ID: 7, NotifyUserID: &notifyUserID},
		},
		getTrunkByDBCompleted: make(chan struct{}, 1),
	}

	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "1001", "1002", "sip-call-busy-push")
	incomingSess.SetSIPAuthContext("trunk", "", 7, "sip.example.com", "1002", "secret", 5060)

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, trunkMgr, nil)
	srv.SetPushService(&push.Service{})
	busyClient := &WSClient{
		send:            make(chan []byte, 8),
		trunkResolved:   true,
		resolvedTrunkID: 7,
		availability:    clientAvailabilityBusy,
		callState:       "incall",
	}
	srv.wsConnections[busyClient] = struct{}{}

	srv.NotifyIncomingCall(incomingSess.ID, "1001", "1002", 7)

	if sipMaker.rejectCount != 1 || sipMaker.lastReject != "busy" {
		t.Fatalf("expected busy reject once, got count=%d reason=%q", sipMaker.rejectCount, sipMaker.lastReject)
	}
	select {
	case <-trunkMgr.getTrunkByDBCompleted:
		t.Fatal("did not expect push DB lookup for busy matching client")
	case <-time.After(100 * time.Millisecond):
	}
	if trunkMgr.getTrunkByDBCalls != 0 {
		t.Fatalf("expected no push DB lookup, got %d", trunkMgr.getTrunkByDBCalls)
	}
}

func TestNotifyIncomingCall_NoOnlineNoPushRejectsOffline480Reason(t *testing.T) {
	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "1001", "1002", "sip-call-offline")
	incomingSess.SetSIPAuthContext("trunk", "", 8, "sip.example.com", "1002", "secret", 5060)

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)

	srv.NotifyIncomingCall(incomingSess.ID, "1001", "1002", 8)

	if sipMaker.rejectCount != 1 || sipMaker.lastReject != "offline" {
		t.Fatalf("expected offline reject once, got count=%d reason=%q", sipMaker.rejectCount, sipMaker.lastReject)
	}
	if _, ok := mgr.GetSession(incomingSess.ID); ok {
		t.Fatalf("expected offline incoming session deleted")
	}
}

func TestNotifyIncomingCall_PushPathTimesOutNoAnswer(t *testing.T) {
	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "1001", "1002", "sip-call-timeout")
	incomingSess.SetSIPAuthContext("trunk", "", 9, "sip.example.com", "1002", "secret", 5060)

	notifyUserID := "user-1"
	trunkMgr := &incomingNotifyTestTrunkManager{
		trunkByID: map[int64]*sip.Trunk{
			9: {ID: 9, NotifyUserID: &notifyUserID},
		},
	}
	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(
		config.APIConfig{IncomingRingTimeoutSeconds: 1},
		config.TURNConfig{},
		config.GatewayConfig{},
		config.TranslatorConfig{},
		mgr,
		sipMaker,
		nil,
		trunkMgr,
		nil,
	)
	srv.pushService = push.NewService(nil, nil)

	srv.NotifyIncomingCall(incomingSess.ID, "1001", "1002", 9)

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if sipMaker.rejectCount == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if sipMaker.rejectCount != 1 || sipMaker.lastReject != "no_answer" {
		t.Fatalf("expected no_answer timeout reject once, got count=%d reason=%q", sipMaker.rejectCount, sipMaker.lastReject)
	}
	if _, ok := mgr.GetSession(incomingSess.ID); ok {
		t.Fatalf("expected timed out incoming session deleted")
	}
}

func TestHandleWSAcceptThenRejectSendsExactlyOneFinalResponse(t *testing.T) {
	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "1001", "1002", "sip-call-race")
	webrtcSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create webrtc session: %v", err)
	}

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{sessionID: webrtcSess.ID, send: make(chan []byte, 8)}

	srv.handleWSAccept(client, WSMessage{Type: "accept", SessionID: incomingSess.ID})
	srv.handleWSReject(client, WSMessage{Type: "reject", SessionID: incomingSess.ID, Reason: "busy"})

	if sipMaker.acceptCount != 1 {
		t.Fatalf("expected AcceptCall once, got %d", sipMaker.acceptCount)
	}
	if sipMaker.rejectCount != 0 {
		t.Fatalf("expected RejectCall not to run after accept, got %d", sipMaker.rejectCount)
	}
}

func TestHandleWSAcceptRejectsWithoutWebRTCSession(t *testing.T) {
	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "1001", "1002", "sip-call-no-webrtc")

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSAccept(client, WSMessage{Type: "accept", SessionID: incomingSess.ID})

	if sipMaker.acceptCount != 0 {
		t.Fatalf("expected AcceptCall not to run without WebRTC session, got %d", sipMaker.acceptCount)
	}
	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "error" {
		t.Fatalf("expected one error message, got %+v", msgs)
	}
}

func TestHandleWSHangup_CancelsOutboundBeforeDialog(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetState(session.StateRinging)

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSHangup(client, WSMessage{Type: "hangup", SessionID: sess.ID})
	srv.handleWSHangup(client, WSMessage{Type: "hangup", SessionID: sess.ID})

	if sipMaker.cancelCount != 1 {
		t.Fatalf("expected CancelPendingCall once, got %d", sipMaker.cancelCount)
	}
	if sipMaker.hangupCount != 0 || sipMaker.rejectCount != 0 {
		t.Fatalf("expected no BYE/reject for pending outbound, got hangup=%d reject=%d", sipMaker.hangupCount, sipMaker.rejectCount)
	}
}

func TestHandleWSHangup_UsesByeForActiveDialog(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetState(session.StateActive)
	sess.SetSIPDialogState("from-tag", "to-tag", "<sip:1002@example.com>", "example.com", 5060, 1, nil)

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSHangup(client, WSMessage{Type: "hangup", SessionID: sess.ID})

	if sipMaker.hangupCount != 1 {
		t.Fatalf("expected Hangup/BYE once, got %d", sipMaker.hangupCount)
	}
	if sipMaker.cancelCount != 0 || sipMaker.rejectCount != 0 {
		t.Fatalf("expected no CANCEL/reject for active dialog, got cancel=%d reject=%d", sipMaker.cancelCount, sipMaker.rejectCount)
	}
}

func TestHandleWSHangup_RejectsIncomingBeforeAccept(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetState(session.StateIncoming)

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSHangup(client, WSMessage{Type: "hangup", SessionID: sess.ID})

	if sipMaker.rejectCount != 1 || sipMaker.lastReject != "busy" {
		t.Fatalf("expected incoming hangup to reject busy once, got count=%d reason=%q", sipMaker.rejectCount, sipMaker.lastReject)
	}
	if sipMaker.cancelCount != 0 || sipMaker.hangupCount != 0 {
		t.Fatalf("expected no CANCEL/BYE for incoming reject, got cancel=%d hangup=%d", sipMaker.cancelCount, sipMaker.hangupCount)
	}
}

func TestNotifyIncomingCall_SendsOnlyClientsResolvedOnSameTrunk(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	clientA := &WSClient{sessionID: "a", trunkResolved: true, resolvedTrunkID: 1, send: make(chan []byte, 8)}
	clientB := &WSClient{sessionID: "b", trunkResolved: true, resolvedTrunkID: 1, send: make(chan []byte, 8)}
	clientC := &WSClient{sessionID: "c", trunkResolved: true, resolvedTrunkID: 2, send: make(chan []byte, 8)}
	clientD := &WSClient{sessionID: "d", trunkResolved: false, resolvedTrunkID: 1, send: make(chan []byte, 8)}

	srv.mu.Lock()
	srv.wsConnections[clientA] = struct{}{}
	srv.wsConnections[clientB] = struct{}{}
	srv.wsConnections[clientC] = struct{}{}
	srv.wsConnections[clientD] = struct{}{}
	srv.mu.Unlock()

	srv.NotifyIncomingCall("incoming-123", "sip:alice@example.com", "sip:bob@example.com", 1)

	msgsA := readWSMessages(t, clientA.send)
	msgsB := readWSMessages(t, clientB.send)
	msgsC := readWSMessages(t, clientC.send)
	msgsD := readWSMessages(t, clientD.send)
	if len(msgsA) != 1 || len(msgsB) != 1 || len(msgsC) != 0 || len(msgsD) != 0 {
		t.Fatalf("expected incoming only for same-trunk resolved clients, got A=%d B=%d C=%d D=%d", len(msgsA), len(msgsB), len(msgsC), len(msgsD))
	}

	msg := msgsA[0]
	if msg.Type != "incoming" {
		t.Fatalf("resolved client expected incoming, got %s", msg.Type)
	}
	if msg.SessionID != "incoming-123" {
		t.Fatalf("resolved client expected session incoming-123, got %s", msg.SessionID)
	}
	if msg.From != "sip:alice@example.com" || msg.To != "sip:bob@example.com" {
		t.Fatalf("resolved client unexpected from/to: from=%s to=%s", msg.From, msg.To)
	}
}

func TestNotifyIncomingCall_TargetsResolvedTrunkID(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	clientTrunk1 := &WSClient{sessionID: "trunk-1-client", trunkResolved: true, resolvedTrunkID: 1, send: make(chan []byte, 8)}
	clientTrunk2 := &WSClient{sessionID: "trunk-2-client", trunkResolved: true, resolvedTrunkID: 2, send: make(chan []byte, 8)}

	srv.mu.Lock()
	srv.wsConnections[clientTrunk1] = struct{}{}
	srv.wsConnections[clientTrunk2] = struct{}{}
	srv.mu.Unlock()

	srv.NotifyIncomingCall("incoming-trunk-2", "sip:alice@example.com", "sip:00025@203.151.21.121:5090", 2)

	msgs1 := readWSMessages(t, clientTrunk1.send)
	msgs2 := readWSMessages(t, clientTrunk2.send)
	if len(msgs1) != 0 {
		t.Fatalf("expected trunk-1 client to receive no incoming, got %d", len(msgs1))
	}
	if len(msgs2) != 1 {
		t.Fatalf("expected trunk-2 client to receive one incoming, got %d", len(msgs2))
	}
	if msgs2[0].Type != "incoming" || msgs2[0].SessionID != "incoming-trunk-2" {
		t.Fatalf("unexpected message for trunk-2 client: %+v", msgs2[0])
	}
}

func TestNotifyIncomingCall_IdleClientAlsoDispatchesPushWhenTargetExists(t *testing.T) {
	notifyUserID := "user-1"
	trunkMgr := &incomingNotifyTestTrunkManager{
		trunkByID: map[int64]*sip.Trunk{
			1: {ID: 1, NotifyUserID: &notifyUserID},
		},
		getTrunkByDBCompleted: make(chan struct{}, 2),
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, nil)
	srv.SetPushService(&push.Service{})
	client := &WSClient{
		sessionID:       "mobile-client",
		trunkResolved:   true,
		resolvedTrunkID: 1,
		availability:    clientAvailabilityIdle,
		callState:       string(session.StateNew),
		send:            make(chan []byte, 8),
	}
	srv.wsConnections[client] = struct{}{}

	srv.NotifyIncomingCall("incoming-ws-push", "sip:alice@example.com", "sip:bob@example.com", 1)

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "incoming" || msgs[0].SessionID != "incoming-ws-push" {
		t.Fatalf("expected one WS incoming message, got %+v", msgs)
	}
	waitForDBLookups(t, trunkMgr, 2)
	if trunkMgr.getTrunkByDBCalls != 2 {
		t.Fatalf("expected push target check and dispatch DB lookups, got %d", trunkMgr.getTrunkByDBCalls)
	}
}

func TestNotifyIncomingCall_IdleClientWithoutPushTargetSendsWSOnly(t *testing.T) {
	trunkMgr := &incomingNotifyTestTrunkManager{
		trunkByID: map[int64]*sip.Trunk{
			1: {ID: 1, NotifyUserID: nil},
		},
		getTrunkByDBCompleted: make(chan struct{}, 2),
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, nil)
	srv.SetPushService(&push.Service{})
	client := &WSClient{
		sessionID:       "mobile-client",
		trunkResolved:   true,
		resolvedTrunkID: 1,
		availability:    clientAvailabilityIdle,
		callState:       string(session.StateNew),
		send:            make(chan []byte, 8),
	}
	srv.wsConnections[client] = struct{}{}

	srv.NotifyIncomingCall("incoming-ws-only", "sip:alice@example.com", "sip:bob@example.com", 1)

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "incoming" || msgs[0].SessionID != "incoming-ws-only" {
		t.Fatalf("expected one WS incoming message, got %+v", msgs)
	}
	waitForDBLookups(t, trunkMgr, 1)
	select {
	case <-trunkMgr.getTrunkByDBCompleted:
		t.Fatal("did not expect dispatch DB lookup when push target is missing")
	case <-time.After(100 * time.Millisecond):
	}
	if trunkMgr.getTrunkByDBCalls != 1 {
		t.Fatalf("expected only push target check DB lookup, got %d", trunkMgr.getTrunkByDBCalls)
	}
}

func TestNotifyIncomingCancel_SendsOnlyClientsResolvedOnSameTrunk(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	clientA := &WSClient{sessionID: "a", trunkResolved: true, resolvedTrunkID: 1, send: make(chan []byte, 8)}
	clientB := &WSClient{sessionID: "b", trunkResolved: true, resolvedTrunkID: 1, send: make(chan []byte, 8)}
	clientC := &WSClient{sessionID: "c", trunkResolved: true, resolvedTrunkID: 2, send: make(chan []byte, 8)}
	clientD := &WSClient{sessionID: "d", trunkResolved: false, resolvedTrunkID: 1, send: make(chan []byte, 8)}

	srv.mu.Lock()
	srv.wsConnections[clientA] = struct{}{}
	srv.wsConnections[clientB] = struct{}{}
	srv.wsConnections[clientC] = struct{}{}
	srv.wsConnections[clientD] = struct{}{}
	srv.mu.Unlock()

	srv.NotifyIncomingCancel("incoming-123", 1, "caller_cancelled")

	msgsA := readWSMessages(t, clientA.send)
	msgsB := readWSMessages(t, clientB.send)
	msgsC := readWSMessages(t, clientC.send)
	msgsD := readWSMessages(t, clientD.send)
	if len(msgsA) != 1 || len(msgsB) != 1 || len(msgsC) != 0 || len(msgsD) != 0 {
		t.Fatalf("expected cancel only for same-trunk resolved clients, got A=%d B=%d C=%d D=%d", len(msgsA), len(msgsB), len(msgsC), len(msgsD))
	}

	msg := msgsA[0]
	if msg.Type != "cancel" {
		t.Fatalf("resolved client expected cancel, got %s", msg.Type)
	}
	if msg.SessionID != "incoming-123" {
		t.Fatalf("resolved client expected session incoming-123, got %s", msg.SessionID)
	}
	if msg.Reason != "caller_cancelled" {
		t.Fatalf("resolved client expected reason caller_cancelled, got %s", msg.Reason)
	}
}

func TestNotifySIPMessage_TargetsMatchingSessionOnly(t *testing.T) {
	mgr := newTestSessionManager()
	targetSession, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create target session: %v", err)
	}
	targetSession.SetCallInfo("outbound", "1100200372057", "14131", "call-target")
	targetSession.SetState(session.StateActive)

	otherSession, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create other session: %v", err)
	}
	otherSession.SetCallInfo("outbound", "1100200999999", "14132", "call-other")
	otherSession.SetState(session.StateActive)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	targetClient := &WSClient{sessionID: targetSession.ID, send: make(chan []byte, 8), ConnectedAt: time.Now()}
	otherClient := &WSClient{sessionID: otherSession.ID, send: make(chan []byte, 8), ConnectedAt: time.Now()}

	srv.mu.Lock()
	srv.wsClients[targetSession.ID] = targetClient
	srv.wsClients[otherSession.ID] = otherClient
	srv.wsConnections[targetClient] = struct{}{}
	srv.wsConnections[otherClient] = struct{}{}
	srv.mu.Unlock()

	srv.NotifySIPMessage("sip:1100200372057@203.151.21.121:5060", "00025", "hello", "text/plain")

	targetMsgs := readWSMessages(t, targetClient.send)
	otherMsgs := readWSMessages(t, otherClient.send)
	if len(targetMsgs) != 1 {
		t.Fatalf("expected target client to receive one message, got %d", len(targetMsgs))
	}
	if len(otherMsgs) != 0 {
		t.Fatalf("expected other client to receive no message, got %d", len(otherMsgs))
	}
	if targetMsgs[0].Type != "message" || targetMsgs[0].SessionID != targetSession.ID {
		t.Fatalf("unexpected target message: %+v", targetMsgs[0])
	}
	if targetMsgs[0].From != "00025" || targetMsgs[0].To != "sip:1100200372057@203.151.21.121:5060" || targetMsgs[0].Body != "hello" {
		t.Fatalf("unexpected message payload: %+v", targetMsgs[0])
	}
}

func TestNotifySIPMessage_DedupesStaleWSClientMapEntries(t *testing.T) {
	mgr := newTestSessionManager()
	targetSession, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create target session: %v", err)
	}
	targetSession.SetCallInfo("outbound", "1100200372057", "14131", "call-target")
	targetSession.SetState(session.StateActive)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{sessionID: targetSession.ID, send: make(chan []byte, 8), ConnectedAt: time.Now()}
	staleClient := &WSClient{sessionID: targetSession.ID, send: make(chan []byte, 8), ConnectedAt: time.Now().Add(-time.Minute)}

	srv.mu.Lock()
	srv.wsClients[targetSession.ID] = client
	srv.wsClients["old-offer-session"] = client
	srv.wsClients["older-offer-session"] = staleClient
	srv.wsConnections[client] = struct{}{}
	srv.wsConnections[staleClient] = struct{}{}
	srv.mu.Unlock()

	srv.NotifySIPMessage("sip:1100200372057@203.151.21.121:5060", "00025", "hello", "text/plain")

	msgs := readWSMessages(t, client.send)
	staleMsgs := readWSMessages(t, staleClient.send)
	if len(msgs) != 1 {
		t.Fatalf("expected canonical client to receive one message, got %d", len(msgs))
	}
	if len(staleMsgs) != 0 {
		t.Fatalf("expected stale client to receive no message, got %d", len(staleMsgs))
	}
	if msgs[0].SessionID != targetSession.ID {
		t.Fatalf("expected message session %s, got %s", targetSession.ID, msgs[0].SessionID)
	}
}

func TestNotifyIncomingCall_PushLookupUsesDBPath(t *testing.T) {
	trunkMgr := &incomingNotifyTestTrunkManager{
		trunkByID: map[int64]*sip.Trunk{
			1: {
				ID:           1,
				NotifyUserID: nil,
			},
		},
		getTrunkByDBCompleted: make(chan struct{}, 1),
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, nil)
	srv.SetPushService(&push.Service{})

	srv.NotifyIncomingCall("incoming-db-lookup", "sip:alice@example.com", "sip:bob@example.com", 1)

	select {
	case <-trunkMgr.getTrunkByDBCompleted:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected DB lookup to complete for push path")
	}

	if trunkMgr.getTrunkByDBCalls != 1 {
		t.Fatalf("expected GetTrunkByIDFromDB to be called once, got %d", trunkMgr.getTrunkByDBCalls)
	}
	if trunkMgr.getTrunkByIDCalls != 0 {
		t.Fatalf("expected cache GetTrunkByID not to be used, got %d", trunkMgr.getTrunkByIDCalls)
	}
}

func TestNotifyIncomingCall_PushLookupDBErrorStillAvoidsCachePath(t *testing.T) {
	trunkMgr := &incomingNotifyTestTrunkManager{
		trunkByID:             map[int64]*sip.Trunk{},
		getTrunkByDBErr:       errors.New("db unavailable"),
		getTrunkByDBCompleted: make(chan struct{}, 1),
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, trunkMgr, nil)
	srv.SetPushService(&push.Service{})

	srv.NotifyIncomingCall("incoming-db-error", "sip:alice@example.com", "sip:bob@example.com", 1)

	select {
	case <-trunkMgr.getTrunkByDBCompleted:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected DB lookup attempt for push path")
	}

	if trunkMgr.getTrunkByDBCalls != 1 {
		t.Fatalf("expected GetTrunkByIDFromDB to be called once, got %d", trunkMgr.getTrunkByDBCalls)
	}
	if trunkMgr.getTrunkByIDCalls != 0 {
		t.Fatalf("expected cache GetTrunkByID not to be used, got %d", trunkMgr.getTrunkByIDCalls)
	}
}

func TestSelectIncomingPushRoute_AndroidUsesFCMOnly(t *testing.T) {
	notifyUserID := "user-1"
	platform := "android"
	appID := config.DefaultTrunkPNAppID
	pnType := trunkPNType
	token := "D6F5DF83B03398129B4AC01DFE5971662B46130F3F5424AF93CF0A8C02A74CCF"
	trunk := &sip.Trunk{
		ID:                 1,
		NotifyUserID:       &notifyUserID,
		LastOnlinePlatform: &platform,
		PNAppID:            &appID,
		PNType:             &pnType,
		PNToken:            &token,
	}

	route := selectIncomingPushRoute(trunk, true, config.DefaultTrunkPNAppID)

	if !route.SendFCM || route.SendAPNS {
		t.Fatalf("expected android route to use FCM only, got %+v", route)
	}
}

func TestSelectIncomingPushRoute_IOSUsesAPNSOnly(t *testing.T) {
	notifyUserID := "user-1"
	platform := "ios"
	appID := config.DefaultTrunkPNAppID
	pnType := trunkPNType
	token := "D6F5DF83B03398129B4AC01DFE5971662B46130F3F5424AF93CF0A8C02A74CCF"
	trunk := &sip.Trunk{
		ID:                 1,
		NotifyUserID:       &notifyUserID,
		LastOnlinePlatform: &platform,
		PNAppID:            &appID,
		PNType:             &pnType,
		PNToken:            &token,
	}

	route := selectIncomingPushRoute(trunk, true, config.DefaultTrunkPNAppID)

	if !route.SendAPNS || route.SendFCM {
		t.Fatalf("expected ios route to use APNS only, got %+v", route)
	}
}

func TestSelectIncomingPushRoute_UnknownPlatformKeepsFallback(t *testing.T) {
	notifyUserID := "user-1"
	appID := config.DefaultTrunkPNAppID
	pnType := trunkPNType
	token := "D6F5DF83B03398129B4AC01DFE5971662B46130F3F5424AF93CF0A8C02A74CCF"
	trunk := &sip.Trunk{
		ID:           1,
		NotifyUserID: &notifyUserID,
		PNAppID:      &appID,
		PNType:       &pnType,
		PNToken:      &token,
	}

	route := selectIncomingPushRoute(trunk, true, config.DefaultTrunkPNAppID)

	if !route.SendAPNS || !route.SendFCM || !route.UnknownPlatformFallback {
		t.Fatalf("expected unknown platform fallback to use both targets, got %+v", route)
	}
}

func TestSelectIncomingPushRoute_UsesConfiguredPNAppIDForAPNSTarget(t *testing.T) {
	notifyUserID := "user-1"
	platform := "ios"
	appID := "th.or.ttrs.video.staging"
	pnType := trunkPNType
	token := "D6F5DF83B03398129B4AC01DFE5971662B46130F3F5424AF93CF0A8C02A74CCF"
	trunk := &sip.Trunk{
		ID:                 1,
		NotifyUserID:       &notifyUserID,
		LastOnlinePlatform: &platform,
		PNAppID:            &appID,
		PNType:             &pnType,
		PNToken:            &token,
	}

	route := selectIncomingPushRoute(trunk, true, "th.or.ttrs.video.staging")

	if !route.SendAPNS || route.SendFCM {
		t.Fatalf("expected configured app ID to enable APNS route, got %+v", route)
	}
}

func TestIncomingAcceptThenHangup_UsesSessionWithDialogState(t *testing.T) {
	mgr := newTestSessionManager()
	incomingSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create incoming session: %v", err)
	}
	incomingSess.SetState(session.StateIncoming)
	incomingSess.SetCallInfo("inbound", "sip:00025@203.150.245.42:5060", "sip:1100@203.151.21.121:5060", "sip-call-3")

	sipMaker := &incomingTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	webrtcSess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create webrtc session: %v", err)
	}
	client := &WSClient{sessionID: webrtcSess.ID, send: make(chan []byte, 16)}

	srv.handleWSAccept(client, WSMessage{
		Type:      "accept",
		SessionID: incomingSess.ID,
	})
	srv.handleWSHangup(client, WSMessage{
		Type:      "hangup",
		SessionID: webrtcSess.ID,
	})

	if sipMaker.hangupCount != 1 {
		t.Fatalf("expected Hangup once, got %d", sipMaker.hangupCount)
	}
	if sipMaker.lastHangup == nil {
		t.Fatalf("expected hangup session to be captured")
	}
	fromTag, toTag, _, _, _, domain, port := sipMaker.lastHangup.GetSIPDialogState()
	if fromTag == "" || toTag == "" {
		t.Fatalf("expected dialog tags to be present on hangup session")
	}
	if domain == "" || port == 0 {
		t.Fatalf("expected dialog domain/port to be present on hangup session, got domain=%q port=%d", domain, port)
	}
}

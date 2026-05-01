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

	client1 := &WSClient{send: make(chan []byte, 8)}
	client2 := &WSClient{send: make(chan []byte, 8)}

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
		t.Fatalf("expected error for second accept, got %s", msgs2[0].Type)
	}
	if msgs2[0].Error != "Call already accepted by another client" {
		t.Fatalf("unexpected second accept error: %q", msgs2[0].Error)
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
	client := &WSClient{send: make(chan []byte, 16)}

	srv.handleWSAccept(client, WSMessage{
		Type:      "accept",
		SessionID: incomingSess.ID,
	})
	srv.handleWSHangup(client, WSMessage{
		Type:      "hangup",
		SessionID: incomingSess.ID,
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

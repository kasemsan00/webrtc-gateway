package api

import (
	"strings"
	"testing"
	"time"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/sip"
)

type stubSIPCallMaker struct {
	makeCallCount int
	lastDest      string
	lastFrom      string
	lastSessionID string
	makeCallErr   error
}

func (s *stubSIPCallMaker) MakeCall(destination, from string, sess *session.Session) error {
	s.makeCallCount++
	s.lastDest = destination
	s.lastFrom = from
	if sess != nil {
		s.lastSessionID = sess.ID
	}
	return s.makeCallErr
}

func (s *stubSIPCallMaker) CancelPendingCall(sess *session.Session) error         { return nil }
func (s *stubSIPCallMaker) Hangup(sess *session.Session) error                    { return nil }
func (s *stubSIPCallMaker) SendDTMF(sess *session.Session, digits string) error   { return nil }
func (s *stubSIPCallMaker) AcceptCall(sess *session.Session) error                { return nil }
func (s *stubSIPCallMaker) RejectCall(sess *session.Session, reason string) error { return nil }
func (s *stubSIPCallMaker) SendMessage(destination, from, body, contentType string) error {
	return nil
}
func (s *stubSIPCallMaker) SendMessageToSession(sess *session.Session, body, contentType string) error {
	return nil
}
func (s *stubSIPCallMaker) TriggerSwitchMessage(body, callerURI string) error {
	return nil
}

func waitForMakeCallCount(t *testing.T, maker *stubSIPCallMaker, want int) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if maker.makeCallCount == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected MakeCall count %d, got %d", want, maker.makeCallCount)
}

func TestHandleWSCallRejectsPublicIdentityChange(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetSIPAuthContext("public", "userA@example.com", 0, "example.com", "userA", "old-secret", 5060)

	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1001",
		From:        "callerA",
		SIPDomain:   "example.com",
		SIPUsername: "userB",
		SIPPassword: "new-secret",
		SIPPort:     5060,
	})

	if sipMaker.makeCallCount != 0 {
		t.Fatalf("expected MakeCall not to be called, got %d", sipMaker.makeCallCount)
	}

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 websocket message, got %d", len(msgs))
	}
	if msgs[0].Type != "error" {
		t.Fatalf("expected error message, got %s", msgs[0].Type)
	}
	if !strings.Contains(msgs[0].Error, "Public SIP identity changed") {
		t.Fatalf("unexpected error message: %q", msgs[0].Error)
	}
}

func TestHandleWSCallAllowsSamePublicIdentity(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetSIPAuthContext("public", "userA@example.com", 0, "example.com", "userA", "old-secret", 5060)

	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1002",
		From:        "callerA",
		SIPDomain:   "example.com",
		SIPUsername: "userA",
		SIPPassword: "new-secret",
		SIPPort:     5060,
	})

	waitForMakeCallCount(t, sipMaker, 1)
	if sipMaker.lastSessionID != sess.ID {
		t.Fatalf("expected MakeCall session %s, got %s", sess.ID, sipMaker.lastSessionID)
	}

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 websocket message, got %d", len(msgs))
	}
	if msgs[0].Type != "state" {
		t.Fatalf("expected state message, got %s", msgs[0].Type)
	}
}

func TestHandleWSCall_PublicOnlyAllowsPublicCredentials(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1002",
		From:        "callerA",
		SIPDomain:   "example.com",
		SIPUsername: "userA",
		SIPPassword: "secret",
		SIPPort:     5060,
	})

	waitForMakeCallCount(t, sipMaker, 1)
	mode, _, _, domain, username, password, port := sess.GetSIPAuthContext()
	if mode != "public" || domain != "example.com" || username != "userA" || password != "secret" || port != 5060 {
		t.Fatalf("expected public SIP auth context, got mode=%q domain=%q username=%q password=%q port=%d", mode, domain, username, password, port)
	}
}

func TestHandleWSCall_PublicOnlyRejectsMissingPublicCredentials(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1002",
	})

	if sipMaker.makeCallCount != 0 {
		t.Fatalf("expected MakeCall not to be called, got %d", sipMaker.makeCallCount)
	}
	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "error" || !strings.Contains(msgs[0].Error, "Public SIP credentials required") {
		t.Fatalf("expected public credentials error, got %+v", msgs)
	}
}

func TestHandleWSCall_PublicOnlyRejectsTrunkFields(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1002",
		TrunkID:     42,
	})

	if sipMaker.makeCallCount != 0 {
		t.Fatalf("expected MakeCall not to be called, got %d", sipMaker.makeCallCount)
	}
	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "error" || !strings.Contains(msgs[0].Error, "Trunk calls require authenticated WebSocket") {
		t.Fatalf("expected trunk auth error, got %+v", msgs)
	}
}

func TestHandleWSMessage_PublicOnlyRejectsTrunkResolve(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, newTestSessionManager(), nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true}

	srv.handleWSMessage(client, []byte(`{"type":"trunk_resolve","sessionId":"s1","trunkId":42}`))

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "error" || !strings.Contains(msgs[0].Error, "requires authenticated WebSocket") {
		t.Fatalf("expected authenticated websocket error, got %+v", msgs)
	}
}

func TestHandleWSMessage_PublicOnlyRejectsSessionActionBeforeOwnSession(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true}

	srv.handleWSMessage(client, []byte(`{"type":"hangup","sessionId":"`+sess.ID+`"}`))

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "error" || !strings.Contains(msgs[0].Error, "session is not established") {
		t.Fatalf("expected public session ownership error, got %+v", msgs)
	}
}

func TestHandleWSMessage_PublicOnlyResumeRequiresPublicSession(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true}

	srv.handleWSMessage(client, []byte(`{"type":"resume","sessionId":"`+sess.ID+`"}`))

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "error" || !strings.Contains(msgs[0].Error, "resume public SIP sessions") {
		t.Fatalf("expected public resume guard error, got %+v", msgs)
	}
}

func TestHandleWSMessage_PublicOnlyResumeAllowsPublicSession(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetSIPAuthContext("public", "userA@example.com", 0, "example.com", "userA", "secret", 5060)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true}

	srv.handleWSMessage(client, []byte(`{"type":"resume","sessionId":"`+sess.ID+`"}`))

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "resume_failed" {
		t.Fatalf("expected resume handler to run for public session, got %+v", msgs)
	}
	if strings.Contains(msgs[0].Error, "Public WebSocket") {
		t.Fatalf("did not expect public websocket guard error, got %+v", msgs)
	}
}

func TestHandleWSMessage_PublicOnlyAllowsTranslateForPublicSession(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetSIPAuthContext("public", "userA@example.com", 0, "example.com", "userA", "secret", 5060)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

	srv.handleWSMessage(client, []byte(`{"type":"translate","sessionId":"`+sess.ID+`","sourceLang":"en","targetLang":"th"}`))

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "error" || !strings.Contains(msgs[0].Error, "Translator not available") {
		t.Fatalf("expected translate handler to run and report missing translator, got %+v", msgs)
	}
}

func TestHandleWSMessage_PublicOnlyRejectsTranslateForNonPublicSession(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

	srv.handleWSMessage(client, []byte(`{"type":"translate","sessionId":"`+sess.ID+`","sourceLang":"en","targetLang":"th"}`))

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "error" || !strings.Contains(msgs[0].Error, "public SIP sessions") {
		t.Fatalf("expected public session guard error, got %+v", msgs)
	}
}

func TestHandleWSMessage_PublicOnlyAllowsTranslateStopForPublicSession(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetSIPAuthContext("public", "userA@example.com", 0, "example.com", "userA", "secret", 5060)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

	srv.handleWSMessage(client, []byte(`{"type":"translate_stop","sessionId":"`+sess.ID+`"}`))

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "translate_stop" || msgs[0].State != "disabled" {
		t.Fatalf("expected translate_stop handler to run for public session, got %+v", msgs)
	}
}

func TestHandleWSMessage_PublicOnlyAllowsSendMessageForPublicSession(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetSIPAuthContext("public", "userA@example.com", 0, "example.com", "userA", "secret", 5060)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

	srv.handleWSMessage(client, []byte(`{"type":"send_message","sessionId":"`+sess.ID+`","body":"hello"}`))

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "messageSent" || msgs[0].Body != "hello" {
		t.Fatalf("expected send_message handler to run for public session, got %+v", msgs)
	}
	if strings.Contains(msgs[0].Error, "requires authenticated WebSocket") {
		t.Fatalf("did not expect authenticated websocket guard error, got %+v", msgs)
	}
}

func TestHandleWSMessage_PublicOnlyRejectsSendMessageForNonPublicSession(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true, sessionID: sess.ID}

	srv.handleWSMessage(client, []byte(`{"type":"send_message","sessionId":"`+sess.ID+`","body":"hello"}`))

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "error" || !strings.Contains(msgs[0].Error, "public SIP sessions") {
		t.Fatalf("expected public session guard error, got %+v", msgs)
	}
}

func TestHandleWSMessage_PublicOnlyRejectsSendMessageBeforeOwnSession(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetSIPAuthContext("public", "userA@example.com", 0, "example.com", "userA", "secret", 5060)

	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, nil, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8), publicOnly: true}

	srv.handleWSMessage(client, []byte(`{"type":"send_message","sessionId":"`+sess.ID+`","body":"hello"}`))

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "error" {
		t.Fatalf("expected error before own session is established, got %+v", msgs)
	}
	if !strings.Contains(msgs[0].Error, "Public WebSocket session is not established") &&
		!strings.Contains(msgs[0].Error, "Session ID required") {
		t.Fatalf("expected session ownership guard error, got %+v", msgs)
	}
}

func TestHandleTranslationCaptionSendsOnlyOwningSessionClient(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, nil, nil, nil, nil, nil)
	owner := &WSClient{send: make(chan []byte, 8), sessionID: "session-1"}
	other := &WSClient{send: make(chan []byte, 8), sessionID: "session-2"}
	srv.wsClients["session-1"] = owner
	srv.wsClients["session-2"] = other

	srv.handleTranslationCaption("session-1", session.TranslationCaptionEvent{
		Direction:      "sip_to_webrtc",
		SourceLang:     "th-TH",
		TargetLang:     "en",
		RecognizedText: "สวัสดี",
		TranslatedText: "hello",
		IsFinal:        false,
	})

	msgs := readWSMessages(t, owner.send)
	if len(msgs) != 1 {
		t.Fatalf("expected owner to receive one caption, got %+v", msgs)
	}
	if msgs[0].Type != "translation_caption" ||
		msgs[0].SessionID != "session-1" ||
		msgs[0].Direction != "sip_to_webrtc" ||
		msgs[0].SourceLang != "th-TH" ||
		msgs[0].TargetLang != "en" ||
		msgs[0].RecognizedText != "สวัสดี" ||
		msgs[0].TranslatedText != "hello" ||
		msgs[0].IsFinal == nil ||
		*msgs[0].IsFinal {
		t.Fatalf("unexpected caption message: %+v", msgs[0])
	}
	if len(other.send) != 0 {
		t.Fatalf("expected other client not to receive caption, got %d messages", len(other.send))
	}
}

func TestHandleWSCall_AllowsIdentityChangeForNonPublicMode(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	sess.SetSIPAuthContext("trunk", "", 1, "example.com", "userA", "old-secret", 5060)

	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1003",
		From:        "callerA",
		SIPDomain:   "example.com",
		SIPUsername: "userB",
		SIPPassword: "new-secret",
		SIPPort:     5060,
	})

	waitForMakeCallCount(t, sipMaker, 1)
}

func TestHandleWSCallRejectsTrunkCallWhenNotResolved(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	owner := "gw-1"
	future := time.Now().Add(2 * time.Minute)
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {ID: 42, PublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a", LeaseOwner: &owner, LeaseUntil: &future},
		},
		byPublicID: map[string]int64{
			"8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a": 42,
		},
	}

	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, mgr, sipMaker, nil, trunkMgr, nil)
	client := &WSClient{send: make(chan []byte, 8)}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1004",
		TrunkID:     42,
	})

	if sipMaker.makeCallCount != 0 {
		t.Fatalf("expected MakeCall not to be called, got %d", sipMaker.makeCallCount)
	}

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 websocket message, got %d", len(msgs))
	}
	if msgs[0].Type != "error" {
		t.Fatalf("expected error message, got %s", msgs[0].Type)
	}
	if !strings.Contains(msgs[0].Error, "Trunk must be resolved before placing call") {
		t.Fatalf("unexpected error message: %q", msgs[0].Error)
	}
}

func TestHandleWSCallUsesResolvedTrunkWhenNoAuthFieldsProvided(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	owner := "gw-1"
	future := time.Now().Add(2 * time.Minute)
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {ID: 42, PublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a", LeaseOwner: &owner, LeaseUntil: &future},
		},
	}

	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, mgr, sipMaker, nil, trunkMgr, nil)
	client := &WSClient{
		send:            make(chan []byte, 8),
		trunkResolved:   true,
		resolvedTrunkID: 42,
	}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1004",
	})

	waitForMakeCallCount(t, sipMaker, 1)
	mode, _, trunkID, _, _, _, _ := sess.GetSIPAuthContext()
	if mode != "trunk" || trunkID != 42 {
		t.Fatalf("expected call to use resolved trunk 42, got mode=%q trunkID=%d", mode, trunkID)
	}

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "state" {
		t.Fatalf("expected state message, got %+v", msgs)
	}
}

func TestHandleWSCallRejectsTrunkCallWhenLeaseNotActive(t *testing.T) {
	mgr := newTestSessionManager()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	owner := "gw-1"
	past := time.Now().Add(-1 * time.Minute)
	trunkMgr := &stubResolveTrunkManager{
		byID: map[int64]*sip.Trunk{
			42: {ID: 42, PublicID: "8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a", LeaseOwner: &owner, LeaseUntil: &past},
		},
		byPublicID: map[string]int64{
			"8f6f6d70-2b5a-4fe7-a0d5-9d0af0e90d3a": 42,
		},
	}

	sipMaker := &stubSIPCallMaker{}
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{InstanceID: "gw-1"}, config.TranslatorConfig{}, mgr, sipMaker, nil, trunkMgr, nil)
	client := &WSClient{
		send:            make(chan []byte, 8),
		trunkResolved:   true,
		resolvedTrunkID: 42,
	}

	srv.handleWSCall(client, WSMessage{
		Type:        "call",
		SessionID:   sess.ID,
		Destination: "1005",
		TrunkID:     42,
	})

	if sipMaker.makeCallCount != 0 {
		t.Fatalf("expected MakeCall not to be called, got %d", sipMaker.makeCallCount)
	}

	msgs := readWSMessages(t, client.send)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 websocket messages, got %d", len(msgs))
	}
	if msgs[0].Type != "trunk_not_ready" {
		t.Fatalf("expected first message trunk_not_ready, got %s", msgs[0].Type)
	}
	if msgs[0].Reason != "Trunk lease not active" {
		t.Fatalf("unexpected trunk_not_ready reason: %q", msgs[0].Reason)
	}
	if msgs[1].Type != "error" {
		t.Fatalf("expected second message error, got %s", msgs[1].Type)
	}
	if !strings.Contains(msgs[1].Error, "Trunk not ready: Trunk lease not active") {
		t.Fatalf("unexpected error message: %q", msgs[1].Error)
	}
	if client.trunkResolved {
		t.Fatalf("expected trunkResolved to be reset to false")
	}
	if client.resolvedTrunkID != 0 {
		t.Fatalf("expected resolvedTrunkID reset to 0, got %d", client.resolvedTrunkID)
	}
}

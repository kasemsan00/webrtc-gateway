package api

import (
	"encoding/json"
	"testing"
	"time"

	"k2-gateway/internal/session"
)

func TestWSMessageMidCallRenegotiationContract(t *testing.T) {
	payload := WSMessage{
		Type:            "renegotiate",
		SessionID:       "sess-123",
		RenegotiationID: "reneg-123",
		Reason:          "video_added",
		SDP:             "v=0\r\n",
		MediaDirection:  "sendrecv",
		HasVideo:        "true",
		RequiresAnswer:  true,
		Status:          "pending",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal renegotiate message: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal renegotiate json: %v", err)
	}

	for key, want := range map[string]interface{}{
		"type":            "renegotiate",
		"sessionId":       "sess-123",
		"renegotiationId": "reneg-123",
		"reason":          "video_added",
		"sdp":             "v=0\r\n",
		"mediaDirection":  "sendrecv",
		"hasVideo":        "true",
		"requiresAnswer":  true,
		"status":          "pending",
	} {
		if got := decoded[key]; got != want {
			t.Fatalf("expected %s=%#v, got %#v in %s", key, want, got, string(data))
		}
	}

	var answer WSMessage
	if err := json.Unmarshal([]byte(`{"type":"renegotiate_answer","sessionId":"sess-123","renegotiationId":"reneg-123","sdp":"v=0\r\n","status":"ok"}`), &answer); err != nil {
		t.Fatalf("unmarshal renegotiate_answer: %v", err)
	}
	if answer.Type != "renegotiate_answer" || answer.SessionID != "sess-123" || answer.RenegotiationID != "reneg-123" || answer.Status != "ok" {
		t.Fatalf("unexpected answer payload: %#v", answer)
	}
}

func TestHandleWSRenegotiateAnswerCompletesPendingOperation(t *testing.T) {
	mgr := newTestSessionManager()
	sess := createActiveSession(t, mgr)
	pending, ok := sess.TryBeginMidCallRenegotiation(session.MidCallRenegotiationRequest{
		ID:       "reneg-123",
		Source:   "sip_reinvite",
		Method:   "INVITE",
		OfferSDP: "offer-sdp",
	})
	if !ok {
		t.Fatal("expected pending renegotiation")
	}
	server := &Server{sessionMgr: mgr}
	client := &WSClient{sessionID: sess.ID, send: make(chan []byte, 2)}

	server.handleWSMessage(client, []byte(`{"type":"renegotiate_answer","sessionId":"`+sess.ID+`","renegotiationId":"`+pending.ID+`","sdp":"answer-sdp","status":"ok"}`))

	msg := readWSTestMessage(t, client)
	if msg.Type != "renegotiate_result" || msg.Status != "ok" || msg.RenegotiationID != pending.ID {
		t.Fatalf("unexpected result message: %#v", msg)
	}
	if _, ok := sess.GetPendingMidCallRenegotiation(); ok {
		t.Fatal("expected pending renegotiation to be cleared")
	}
}

func TestHandleWSRenegotiateAnswerRejectsMismatchedID(t *testing.T) {
	mgr := newTestSessionManager()
	sess := createActiveSession(t, mgr)
	if _, ok := sess.TryBeginMidCallRenegotiation(session.MidCallRenegotiationRequest{
		ID:     "reneg-expected",
		Source: "sip_reinvite",
		Method: "INVITE",
	}); !ok {
		t.Fatal("expected pending renegotiation")
	}
	server := &Server{sessionMgr: mgr}
	client := &WSClient{sessionID: sess.ID, send: make(chan []byte, 2)}

	server.handleWSMessage(client, []byte(`{"type":"renegotiate_answer","sessionId":"`+sess.ID+`","renegotiationId":"reneg-wrong","status":"ok"}`))

	msg := readWSTestMessage(t, client)
	if msg.Type != "error" || msg.Error != "Renegotiation not pending or mismatched" {
		t.Fatalf("unexpected mismatch response: %#v", msg)
	}
	if pending, ok := sess.GetPendingMidCallRenegotiation(); !ok || pending.ID != "reneg-expected" {
		t.Fatalf("expected original pending renegotiation to remain, got %#v ok=%v", pending, ok)
	}
}

func TestScheduleMidCallRenegotiationTimeoutFailsPendingOperation(t *testing.T) {
	mgr := newTestSessionManager()
	sess := createActiveSession(t, mgr)
	pending, ok := sess.TryBeginMidCallRenegotiation(session.MidCallRenegotiationRequest{
		ID:      "reneg-timeout",
		Source:  "sip_reinvite",
		Method:  "INVITE",
		Timeout: time.Millisecond,
	})
	if !ok {
		t.Fatal("expected pending renegotiation")
	}
	client := &WSClient{sessionID: sess.ID, send: make(chan []byte, 2)}
	server := &Server{
		sessionMgr: mgr,
		wsClients:  map[string]*WSClient{sess.ID: client},
	}

	server.scheduleMidCallRenegotiationTimeout(sess.ID, pending.ID, time.Millisecond)

	msg := readWSTestMessage(t, client)
	if msg.Type != "renegotiate_result" || msg.Status != "timeout" || msg.Reason != "client_timeout" {
		t.Fatalf("unexpected timeout result: %#v", msg)
	}
	if _, ok := sess.GetPendingMidCallRenegotiation(); ok {
		t.Fatal("expected timeout to clear pending renegotiation")
	}
}

func TestNotifyMidCallRenegotiationSendsAdditiveClientRequest(t *testing.T) {
	client := &WSClient{sessionID: "sess-123", send: make(chan []byte, 2)}
	server := &Server{wsClients: map[string]*WSClient{"sess-123": client}}

	server.NotifyMidCallRenegotiation("sess-123", session.MidCallRenegotiationSnapshot{
		ID:       "reneg-123",
		OfferSDP: "offer-sdp",
		Timeout:  time.Hour,
	}, session.MidCallSDPValidation{
		Video:          session.MidCallMedia{Present: true, Port: 4002, Direction: "sendrecv"},
		HasActiveVideo: true,
	})

	msg := readWSTestMessage(t, client)
	if msg.Type != "renegotiate" || msg.RenegotiationID != "reneg-123" || msg.Reason != "video_added" || !msg.RequiresAnswer {
		t.Fatalf("unexpected renegotiate request: %#v", msg)
	}
	if msg.SDP != "offer-sdp" || msg.HasVideo != "true" || msg.MediaDirection != "sendrecv" {
		t.Fatalf("unexpected renegotiate media fields: %#v", msg)
	}
}

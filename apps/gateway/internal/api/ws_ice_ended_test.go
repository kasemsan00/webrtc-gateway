package api

import (
	"testing"

	"webrtc-sip-gateway/internal/session"
)

// A call whose ICE gave up must not leave the Asterisk leg up: an established
// dialog gets exactly one BYE, and a user hangup already in progress wins.
func TestHandleICEEnded_SendsByeForDialogOnce(t *testing.T) {
	sip := &incomingTestSIPCallMaker{}
	srv := &Server{sipMaker: sip}
	sess := &session.Session{ID: "ice-ended"}
	sess.SetSIPDialogState("local", "remote", "<sip:1002@127.0.0.1>", "127.0.0.1", 5060, 1, nil)

	srv.HandleICEEnded(sess, "ice_grace_expired")
	srv.HandleICEEnded(sess, "ice_failed")

	if sip.hangupCount != 1 || sip.lastHangup != sess {
		t.Fatalf("expected one BYE for the session, got %d", sip.hangupCount)
	}
}

func TestHandleICEEnded_NoDialogNoBye(t *testing.T) {
	sip := &incomingTestSIPCallMaker{}
	srv := &Server{sipMaker: sip}

	srv.HandleICEEnded(&session.Session{ID: "pre-dialog"}, "ice_failed")

	if sip.hangupCount != 0 {
		t.Fatalf("a call without a dialog must not get a BYE, got %d", sip.hangupCount)
	}
}

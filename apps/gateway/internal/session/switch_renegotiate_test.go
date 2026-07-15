package session

import "testing"

func TestTryClaimSwitchVideoRenegotiation_SerializesPerGeneration(t *testing.T) {
	sess := &Session{ID: "switch-reneg", SwitchGeneration: 3}

	if !sess.TryClaimSwitchVideoRenegotiation(3) {
		t.Fatal("expected first claim to succeed")
	}
	if sess.TryClaimSwitchVideoRenegotiation(3) {
		t.Fatal("expected duplicate generation claim to fail")
	}
	if !sess.TryClaimSwitchVideoRenegotiation(4) {
		t.Fatal("expected claim for newer generation to succeed")
	}

	sess.ReleaseSwitchVideoRenegotiationClaim(4)
	if !sess.TryClaimSwitchVideoRenegotiation(4) {
		t.Fatal("expected claim after release for same generation")
	}
}

func TestTryClaimSwitchVideoRenegotiation_BlockedByPendingMidCall(t *testing.T) {
	sess := &Session{ID: "switch-reneg-pending"}
	if _, ok := sess.TryBeginMidCallRenegotiation(MidCallRenegotiationRequest{
		Source: "sip_reinvite",
		Method: "INVITE",
	}); !ok {
		t.Fatal("expected pending mid-call renegotiation")
	}
	if sess.TryClaimSwitchVideoRenegotiation(2) {
		t.Fatal("expected switch claim blocked while mid-call pending")
	}
}

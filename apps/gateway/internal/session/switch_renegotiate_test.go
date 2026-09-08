package session

import "testing"

func TestTryClaimSwitchVideoRenegotiation_SerializesPerGeneration(t *testing.T) {
	sess := &Session{ID: "switch-reneg", SwitchGeneration: 3}

	if !sess.TryClaimSwitchVideoRenegotiation(3) {
		t.Fatal("expected first claim to succeed")
	}
	if !sess.SwitchVideoRenegotiateHold {
		t.Fatal("expected video hold after claim")
	}
	if sess.TryClaimSwitchVideoRenegotiation(3) {
		t.Fatal("expected duplicate generation claim to fail")
	}
	if !sess.TryClaimSwitchVideoRenegotiation(4) {
		t.Fatal("expected claim for newer generation to succeed")
	}

	sess.ReleaseSwitchVideoRenegotiationClaim(4)
	if sess.SwitchVideoRenegotiateHold {
		t.Fatal("expected hold cleared after failed claim release")
	}
	if !sess.TryClaimSwitchVideoRenegotiation(4) {
		t.Fatal("expected claim after release for same generation")
	}
}

func TestIsSwitchVideoRenegotiationSource(t *testing.T) {
	if !IsSwitchVideoRenegotiationSource(MidCallRenegotiationSourceSwitchMessage) ||
		!IsSwitchVideoRenegotiationSource(MidCallRenegotiationSourceSwitchGateRelease) {
		t.Fatal("expected both switch sources to apply answers on the current PeerConnection")
	}
	if IsSwitchVideoRenegotiationSource("sip_reinvite") {
		t.Fatal("SIP re-INVITE must not use in-place switch apply")
	}
}

func TestCompleteSwitchRenegotiationClearsVideoHold(t *testing.T) {
	sess := &Session{ID: "switch-reneg-hold"}
	if !sess.TryClaimSwitchVideoRenegotiation(2) {
		t.Fatal("expected claim")
	}
	started, ok := sess.TryBeginMidCallRenegotiation(MidCallRenegotiationRequest{
		Source: MidCallRenegotiationSourceSwitchMessage,
		Method: "WS",
	})
	if !ok {
		t.Fatal("expected pending switch renegotiation")
	}
	if !sess.CompleteMidCallRenegotiation(started.ID, "v=0") {
		t.Fatal("expected complete")
	}
	if sess.SwitchVideoRenegotiateHold {
		t.Fatal("expected hold cleared after client answer")
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

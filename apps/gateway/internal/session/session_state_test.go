package session

import "testing"

func TestTerminalCleanupStateIgnoresICETransitions(t *testing.T) {
	tests := []struct {
		name           string
		state          SessionState
		terminalAction string
		want           bool
	}{
		{name: "ended", state: StateEnded, want: true},
		{name: "bye", state: StateActive, terminalAction: "bye", want: true},
		{name: "reject", state: StateIncoming, terminalAction: "reject", want: true},
		{name: "cancel", state: StateRinging, terminalAction: "cancel", want: true},
		{name: "end", state: StateActive, terminalAction: "end", want: true},
		{name: "active", state: StateActive, want: false},
		{name: "connecting", state: StateConnecting, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTerminalCleanupState(tt.state, tt.terminalAction); got != tt.want {
				t.Fatalf("isTerminalCleanupState(%q, %q)=%v want %v", tt.state, tt.terminalAction, got, tt.want)
			}
		})
	}
}

func TestApplyICEConnectedCallProgress(t *testing.T) {
	tests := []struct {
		name        string
		current     SessionState
		wantNext    SessionState
		wantReason  string
		wantChanged bool
	}{
		{
			name:        "connecting stays connecting",
			current:     StateConnecting,
			wantNext:    StateConnecting,
			wantReason:  "initial-call",
			wantChanged: false,
		},
		{
			name:        "ringing stays ringing",
			current:     StateRinging,
			wantNext:    StateRinging,
			wantReason:  "initial-call",
			wantChanged: false,
		},
		{
			name:        "reconnecting restores active",
			current:     StateReconnecting,
			wantNext:    StateActive,
			wantReason:  "ice-reconnected",
			wantChanged: true,
		},
		{
			name:        "active unchanged",
			current:     StateActive,
			wantNext:    StateActive,
			wantReason:  "",
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, reason, changed := ApplyICEConnectedCallProgress(tt.current)
			if next != tt.wantNext || reason != tt.wantReason || changed != tt.wantChanged {
				t.Fatalf("got next=%s reason=%q changed=%v want next=%s reason=%q changed=%v",
					next, reason, changed, tt.wantNext, tt.wantReason, tt.wantChanged)
			}
		})
	}
}

func TestOutboundCallAckState(t *testing.T) {
	if got := OutboundCallAckState(StateConnecting); got != StateConnecting {
		t.Fatalf("connecting ack=%s", got)
	}
	if got := OutboundCallAckState(StateNew); got != StateConnecting {
		t.Fatalf("new ack=%s want connecting", got)
	}
	if got := OutboundCallAckState(StateActive); got != StateActive {
		t.Fatalf("already answered ack=%s", got)
	}
	if got := OutboundCallAckState(StateRinging); got != StateRinging {
		t.Fatalf("ringing ack=%s", got)
	}
}

func TestTakeProgressNotifyDedupes(t *testing.T) {
	sess := &Session{ID: "s1", State: StateConnecting}
	if !sess.TakeProgressNotify(StateConnecting) {
		t.Fatal("first connecting notify should be allowed")
	}
	if sess.TakeProgressNotify(StateConnecting) {
		t.Fatal("duplicate connecting notify should be suppressed")
	}
	if !sess.TakeProgressNotify(StateRinging) {
		t.Fatal("ringing transition should be allowed")
	}
	if sess.TakeProgressNotify(StateRinging) {
		t.Fatal("duplicate ringing notify should be suppressed")
	}
	if !sess.TakeProgressNotify(StateActive) {
		t.Fatal("active transition should be allowed")
	}
}

func TestTryMarkRemoteVideoReady(t *testing.T) {
	sess := &Session{ID: "s-media"}
	if sess.TryMarkRemoteVideoReady(false) {
		t.Fatal("must not mark without parameter sets")
	}
	if !sess.TryMarkRemoteVideoReady(true) {
		t.Fatal("first mark with parameter sets should succeed")
	}
	if sess.TryMarkRemoteVideoReady(true) {
		t.Fatal("duplicate video ready must be suppressed")
	}
}

func TestTryMarkRemoteAudioReady(t *testing.T) {
	sess := &Session{ID: "s-audio"}
	if !sess.TryMarkRemoteAudioReady() {
		t.Fatal("first audio ready should succeed")
	}
	if sess.TryMarkRemoteAudioReady() {
		t.Fatal("duplicate audio ready must be suppressed")
	}
}

func TestTryClaimUplinkKeyframeKickOnRemoteJoin(t *testing.T) {
	sess := &Session{ID: "s-uplink-kick"}
	if !sess.TryClaimUplinkKeyframeKickOnRemoteJoin() {
		t.Fatal("first claim should succeed")
	}
	if sess.TryClaimUplinkKeyframeKickOnRemoteJoin() {
		t.Fatal("second claim must be suppressed")
	}
}

func TestKickUplinkKeyframeOnRemoteJoinIfNeeded_OncePerSession(t *testing.T) {
	sess := &Session{ID: "s-uplink-kick-method"}
	if !sess.KickUplinkKeyframeOnRemoteJoinIfNeeded() {
		t.Fatal("first kick should claim")
	}
	// Simulate a later SSRC change attempting the same first-join kick path.
	if sess.KickUplinkKeyframeOnRemoteJoinIfNeeded() {
		t.Fatal("later SSRC change must not re-arm first-join kick")
	}
}

func TestKickUplinkKeyframeOnFirstSIPRTCPIfNeeded_OncePerSession(t *testing.T) {
	sess := &Session{ID: "s-uplink-kick-rtcp"}
	if !sess.KickUplinkKeyframeOnFirstSIPRTCPIfNeeded() {
		t.Fatal("first SIP SR/RR kick should claim")
	}
	if sess.KickUplinkKeyframeOnFirstSIPRTCPIfNeeded() {
		t.Fatal("later SIP SR/RR must not re-arm first-report kick")
	}
}

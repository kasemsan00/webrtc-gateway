package session

import (
	"testing"
	"time"
)

func TestMidCallRenegotiationController_SerializesPendingOperation(t *testing.T) {
	sess := &Session{ID: "midcall-serial"}
	startedAt := time.Now()

	first, ok := sess.TryBeginMidCallRenegotiation(MidCallRenegotiationRequest{
		ID:        "reneg-1",
		Source:    "sip_reinvite",
		Method:    "INVITE",
		OfferSDP:  "v=0\r\n",
		Reason:    "hold",
		StartedAt: startedAt,
	})
	if !ok {
		t.Fatalf("expected first renegotiation to be accepted")
	}
	if first.ID != "reneg-1" || first.Source != "sip_reinvite" || first.Method != "INVITE" || first.Reason != "hold" {
		t.Fatalf("unexpected first snapshot: %+v", first)
	}

	if _, ok := sess.TryBeginMidCallRenegotiation(MidCallRenegotiationRequest{
		ID:       "reneg-2",
		Source:   "sip_update",
		Method:   "UPDATE",
		OfferSDP: "v=0\r\n",
		Reason:   "resume",
	}); ok {
		t.Fatalf("expected concurrent renegotiation to be rejected")
	}

	pending, ok := sess.GetPendingMidCallRenegotiation()
	if !ok {
		t.Fatalf("expected pending renegotiation")
	}
	if pending.ID != "reneg-1" {
		t.Fatalf("expected original pending renegotiation to remain, got %+v", pending)
	}
}

func TestMidCallRenegotiationController_ClearsOnCompletionOrFailure(t *testing.T) {
	sess := &Session{ID: "midcall-clear"}

	started, ok := sess.TryBeginMidCallRenegotiation(MidCallRenegotiationRequest{
		ID:       "reneg-complete",
		Source:   "sip_reinvite",
		Method:   "INVITE",
		OfferSDP: "v=0\r\n",
	})
	if !ok {
		t.Fatalf("expected renegotiation to start")
	}
	if !sess.CompleteMidCallRenegotiation(started.ID, "v=0\r\n") {
		t.Fatalf("expected completion to clear matching renegotiation")
	}
	if _, ok := sess.GetPendingMidCallRenegotiation(); ok {
		t.Fatalf("expected no pending renegotiation after completion")
	}

	failed, ok := sess.TryBeginMidCallRenegotiation(MidCallRenegotiationRequest{
		ID:       "reneg-fail",
		Source:   "sip_update",
		Method:   "UPDATE",
		OfferSDP: "v=0\r\n",
	})
	if !ok {
		t.Fatalf("expected renegotiation to restart after completion")
	}
	if !sess.FailMidCallRenegotiation(failed.ID, 488, "unsupported_codec") {
		t.Fatalf("expected failure to clear matching renegotiation")
	}
	if _, ok := sess.GetPendingMidCallRenegotiation(); ok {
		t.Fatalf("expected no pending renegotiation after failure")
	}
}

func TestMidCallRenegotiationController_TerminalActionWins(t *testing.T) {
	sess := &Session{ID: "midcall-terminal"}
	if !sess.TryBeginTerminalAction("hangup") {
		t.Fatalf("expected terminal action to start")
	}
	if _, ok := sess.TryBeginMidCallRenegotiation(MidCallRenegotiationRequest{
		ID:       "reneg-after-terminal",
		Source:   "sip_reinvite",
		Method:   "INVITE",
		OfferSDP: "v=0\r\n",
	}); ok {
		t.Fatalf("expected terminal action to block renegotiation")
	}

	sess.ClearTerminalAction()
	if _, ok := sess.TryBeginMidCallRenegotiation(MidCallRenegotiationRequest{
		ID:       "reneg-before-terminal",
		Source:   "sip_reinvite",
		Method:   "INVITE",
		OfferSDP: "v=0\r\n",
	}); !ok {
		t.Fatalf("expected renegotiation to start after terminal action cleared")
	}
	if !sess.TryBeginTerminalAction("bye") {
		t.Fatalf("expected terminal action to win while renegotiation is pending")
	}
	if _, ok := sess.GetPendingMidCallRenegotiation(); ok {
		t.Fatalf("expected pending renegotiation to be cleared when terminal action wins")
	}
}

func TestValidateMidCallSDP_AcceptsHoldResumeAndVideoChanges(t *testing.T) {
	tests := []struct {
		name                          string
		sdp                           string
		wantAudioDirection            string
		wantVideoDirection            string
		wantVideoPort                 int
		wantHasActiveVideo            bool
		wantRequiresClientNegotiation bool
	}{
		{
			name:               "hold via sendonly",
			sdp:                "v=0\r\nm=audio 4000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\na=sendonly\r\nm=video 4002 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=sendonly\r\n",
			wantAudioDirection: "sendonly",
			wantVideoDirection: "sendonly",
			wantVideoPort:      4002,
			wantHasActiveVideo: true,
		},
		{
			name:               "resume via sendrecv",
			sdp:                "v=0\r\nm=audio 4000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\na=sendrecv\r\nm=video 4002 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=sendrecv\r\n",
			wantAudioDirection: "sendrecv",
			wantVideoDirection: "sendrecv",
			wantVideoPort:      4002,
			wantHasActiveVideo: true,
		},
		{
			name:                          "video removed by zero port",
			sdp:                           "v=0\r\nm=audio 4000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\na=sendrecv\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=sendrecv\r\n",
			wantAudioDirection:            "sendrecv",
			wantVideoDirection:            "sendrecv",
			wantVideoPort:                 0,
			wantHasActiveVideo:            false,
			wantRequiresClientNegotiation: true,
		},
		{
			name:                          "video inactive",
			sdp:                           "v=0\r\nm=audio 4000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\na=sendrecv\r\nm=video 4002 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=inactive\r\n",
			wantAudioDirection:            "sendrecv",
			wantVideoDirection:            "inactive",
			wantVideoPort:                 4002,
			wantHasActiveVideo:            false,
			wantRequiresClientNegotiation: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateMidCallSDP(tt.sdp)
			if !got.Accept {
				t.Fatalf("expected SDP to be accepted, got status=%d reason=%s", got.StatusCode, got.Reason)
			}
			if got.Audio.Direction != tt.wantAudioDirection {
				t.Fatalf("audio direction = %q, want %q", got.Audio.Direction, tt.wantAudioDirection)
			}
			if got.Video.Direction != tt.wantVideoDirection {
				t.Fatalf("video direction = %q, want %q", got.Video.Direction, tt.wantVideoDirection)
			}
			if got.Video.Port != tt.wantVideoPort {
				t.Fatalf("video port = %d, want %d", got.Video.Port, tt.wantVideoPort)
			}
			if got.HasActiveVideo != tt.wantHasActiveVideo {
				t.Fatalf("HasActiveVideo = %v, want %v", got.HasActiveVideo, tt.wantHasActiveVideo)
			}
			if got.RequiresClientRenegotiation != tt.wantRequiresClientNegotiation {
				t.Fatalf("RequiresClientRenegotiation = %v, want %v", got.RequiresClientRenegotiation, tt.wantRequiresClientNegotiation)
			}
		})
	}
}

func TestValidateMidCallSDP_RejectsUnsupportedOrMalformedSDP(t *testing.T) {
	tests := []struct {
		name       string
		sdp        string
		wantStatus int
		wantReason string
	}{
		{
			name:       "missing opus",
			sdp:        "v=0\r\nm=audio 4000 RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n",
			wantStatus: 488,
			wantReason: "unsupported_audio_codec",
		},
		{
			name:       "missing h264 when video active",
			sdp:        "v=0\r\nm=audio 4000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\nm=video 4002 RTP/AVP 97\r\na=rtpmap:97 VP8/90000\r\n",
			wantStatus: 488,
			wantReason: "unsupported_video_codec",
		},
		{
			name:       "malformed media port",
			sdp:        "v=0\r\nm=audio nope RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\n",
			wantStatus: 488,
			wantReason: "malformed_sdp",
		},
		{
			name:       "empty SDP",
			sdp:        "",
			wantStatus: 488,
			wantReason: "missing_sdp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateMidCallSDP(tt.sdp)
			if got.Accept {
				t.Fatalf("expected SDP to be rejected")
			}
			if got.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", got.StatusCode, tt.wantStatus)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

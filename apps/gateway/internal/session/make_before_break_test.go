package session

import (
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
)

func TestPrepareSwitchPeerConnectionParksLegacyUntilCommit(t *testing.T) {
	old := newGatewayH264Answerer(t)
	sess := &Session{ID: "switch-mbb", PeerConnection: old, State: StateActive}

	if err := sess.PrepareSwitchPeerConnection(config.TURNConfig{}, false); err != nil {
		t.Fatalf("prepare @switch PC failed: %v", err)
	}
	if sess.PeerConnection == nil || sess.PeerConnection == old {
		t.Fatal("expected signaling PeerConnection to be the replacement")
	}
	if sess.legacyPeerConnection != old {
		t.Fatal("expected previous PeerConnection to stay parked")
	}

	replacement := sess.PeerConnection
	sess.commitMakeBeforeBreak(replacement)
	if sess.legacyPeerConnection != nil {
		t.Fatal("expected parked PeerConnection to clear after ICE commit")
	}
	if sess.PeerConnection != replacement {
		t.Fatal("expected signaling PeerConnection to stay on the replacement")
	}
}

func TestAbortMakeBeforeBreakRestoresLegacyPeerConnection(t *testing.T) {
	old := newGatewayH264Answerer(t)
	sess := &Session{ID: "switch-mbb-abort", PeerConnection: old, State: StateActive}

	if err := sess.PrepareSwitchPeerConnection(config.TURNConfig{}, false); err != nil {
		t.Fatalf("prepare @switch PC failed: %v", err)
	}
	replacement := sess.PeerConnection
	if replacement == old {
		t.Fatal("expected a replacement PeerConnection")
	}
	if !sess.AbortMakeBeforeBreak() {
		t.Fatal("expected abort to restore the parked PeerConnection")
	}
	if sess.PeerConnection != old {
		t.Fatal("expected abort to restore the original PeerConnection")
	}
	if sess.legacyPeerConnection != nil {
		t.Fatal("expected parked PeerConnection to clear after abort")
	}
}

func TestWriteAudioToWebRTCFansOutToLegacyTrack(t *testing.T) {
	primary, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"primary-audio",
	)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"legacy-audio",
	)
	if err != nil {
		t.Fatal(err)
	}
	sess := &Session{ID: "switch-mbb-audio", AudioTrack: primary, legacyAudioTrack: legacy}
	pkt := []byte{0x80, 0x6f, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01}
	if err := sess.WriteAudioToWebRTC(pkt); err != nil {
		t.Fatalf("write audio: %v", err)
	}
}

// A second @switch before the first commits re-parks the PeerConnection. The
// one parked first must be closed, not leaked.
func TestSecondSwitchClosesFirstParkedPeerConnection(t *testing.T) {
	original := newGatewayH264Answerer(t)
	sess := &Session{ID: "switch-twice", PeerConnection: original, State: StateActive}

	if err := sess.PrepareSwitchPeerConnection(config.TURNConfig{}, false); err != nil {
		t.Fatalf("first @switch failed: %v", err)
	}
	first := sess.PeerConnection
	if err := sess.PrepareSwitchPeerConnection(config.TURNConfig{}, false); err != nil {
		t.Fatalf("second @switch failed: %v", err)
	}
	t.Cleanup(func() { _ = sess.PeerConnection.Close(); _ = first.Close() })

	if sess.legacyPeerConnection != first {
		t.Fatal("expected the first replacement to be parked now")
	}
	waitForPeerConnectionClosed(t, original)
}

// A resume during an unfinished @switch drops the parked PeerConnection; it
// must be closed and not restorable.
func TestCreateReplacementForResumeClosesParkedPeerConnection(t *testing.T) {
	original := newGatewayH264Answerer(t)
	sess := &Session{ID: "resume-during-switch", PeerConnection: original, State: StateActive}

	if err := sess.PrepareSwitchPeerConnection(config.TURNConfig{}, false); err != nil {
		t.Fatalf("@switch failed: %v", err)
	}
	switchPC := sess.PeerConnection
	resumePC, err := sess.createReplacementPeerConnection(config.TURNConfig{}, false, renegotiateVideoOfferDiagnostics{}, false)
	if err != nil {
		t.Fatalf("resume replacement failed: %v", err)
	}
	t.Cleanup(func() { _ = resumePC.Close() })

	if sess.legacyPeerConnection != nil || sess.legacyAudioTrack != nil {
		t.Fatal("parked PeerConnection and its tracks must be detached")
	}
	if sess.AbortMakeBeforeBreak() {
		t.Fatal("nothing should be left to restore")
	}
	waitForPeerConnectionClosed(t, original)
	waitForPeerConnectionClosed(t, switchPC)
}

func waitForPeerConnectionClosed(t *testing.T, pc *webrtc.PeerConnection) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for pc.ConnectionState() != webrtc.PeerConnectionStateClosed {
		if time.Now().After(deadline) {
			t.Fatalf("PeerConnection not closed, state=%s", pc.ConnectionState())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

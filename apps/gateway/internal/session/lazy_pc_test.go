package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
)

func TestCreateIncomingSessionDefersPeerConnection(t *testing.T) {
	cfg := &config.Config{}
	mgr := NewManager(cfg)

	infoCalled := false
	mgr.SetSIPKeyframeINFOSender(func(_ *Session, _ string) {
		infoCalled = true
	})
	iceEndedCalled := false
	mgr.SetICEEndedHandler(func(_ *Session, _ string) {
		iceEndedCalled = true
	})

	for _, tc := range []struct {
		name   string
		create func() (*Session, error)
	}{
		{
			name: "CreateIncomingSession",
			create: func() (*Session, error) {
				return mgr.CreateIncomingSession(config.TURNConfig{})
			},
		},
		{
			name: "CreateSessionForIncoming",
			create: func() (*Session, error) {
				return mgr.CreateSessionForIncoming(config.TURNConfig{})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sess, err := tc.create()
			if err != nil {
				t.Fatalf("%s failed: %v", tc.name, err)
			}
			defer mgr.DeleteSession(sess.ID)

			if _, ok := mgr.GetSession(sess.ID); !ok {
				t.Fatalf("expected session %s to be registered in manager", sess.ID)
			}
			if sess.PeerConnection != nil || sess.HasPeerConnection() {
				t.Fatalf("expected lazy incoming session to have nil PeerConnection")
			}
			if sess.AudioTrack != nil || sess.HasAudioTrack() {
				t.Fatalf("expected lazy incoming session to have nil AudioTrack")
			}
			if sess.VideoTrack != nil || sess.HasVideoTrack() {
				t.Fatalf("expected lazy incoming session to have nil VideoTrack")
			}
			if sess.IsClosed() {
				t.Fatalf("expected fresh lazy incoming session not to be closed")
			}

			sess.mu.RLock()
			hasInfoHook := sess.sipKeyframeINFO != nil
			hasICEHook := sess.iceEndedHandler != nil
			sess.mu.RUnlock()
			if !hasInfoHook {
				t.Fatalf("expected lazy incoming session to inherit SIPKeyframeINFOSender hook")
			}
			if !hasICEHook {
				t.Fatalf("expected lazy incoming session to inherit ICEEndedHandler hook")
			}
		})
	}

	// Verify eager CreateSession still allocates PeerConnection and tracks immediately.
	eager, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	defer mgr.DeleteSession(eager.ID)
	if eager.PeerConnection == nil || !eager.HasPeerConnection() {
		t.Fatalf("expected eager CreateSession to initialize PeerConnection")
	}
	if !eager.HasAudioTrack() || !eager.HasVideoTrack() {
		t.Fatalf("expected eager CreateSession to initialize audio and video tracks")
	}
	if infoCalled || iceEndedCalled {
		t.Fatalf("hooks should not fire merely from session creation")
	}
}

func TestEnsurePeerConnectionConcurrentSingleInit(t *testing.T) {
	cfg := &config.Config{}
	mgr := NewManager(cfg)

	sess, err := mgr.CreateIncomingSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateIncomingSession failed: %v", err)
	}
	defer mgr.DeleteSession(sess.ID)

	const workers = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, workers)
	pcs := make([]*webrtc.PeerConnection, workers)
	audios := make([]*webrtc.TrackLocalStaticRTP, workers)
	videos := make([]*webrtc.TrackLocalStaticRTP, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			if err := sess.EnsurePeerConnection(); err != nil {
				errs[idx] = err
				return
			}
			sess.mu.RLock()
			pcs[idx] = sess.PeerConnection
			audios[idx] = sess.AudioTrack
			videos[idx] = sess.VideoTrack
			sess.mu.RUnlock()
		}(i)
	}

	close(start)
	wg.Wait()

	var firstPC *webrtc.PeerConnection
	var firstAudio *webrtc.TrackLocalStaticRTP
	var firstVideo *webrtc.TrackLocalStaticRTP
	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d EnsurePeerConnection error: %v", i, errs[i])
		}
		if pcs[i] == nil || audios[i] == nil || videos[i] == nil {
			t.Fatalf("worker %d observed nil PeerConnection or tracks after EnsurePeerConnection", i)
		}
		if i == 0 {
			firstPC = pcs[i]
			firstAudio = audios[i]
			firstVideo = videos[i]
			continue
		}
		if pcs[i] != firstPC {
			t.Fatalf("worker %d observed different PeerConnection pointer (duplicate allocation)", i)
		}
		if audios[i] != firstAudio {
			t.Fatalf("worker %d observed different AudioTrack pointer", i)
		}
		if videos[i] != firstVideo {
			t.Fatalf("worker %d observed different VideoTrack pointer", i)
		}
	}

	// Repeated call is a fast no-op preserving the same PeerConnection.
	if err := sess.EnsurePeerConnection(); err != nil {
		t.Fatalf("second EnsurePeerConnection failed: %v", err)
	}
	if sess.PeerConnection != firstPC {
		t.Fatalf("expected idempotent EnsurePeerConnection to keep the same PeerConnection")
	}

	// Verify the lazily created PeerConnection can negotiate a real WebRTC offer/answer.
	offerer := newLoopbackPeerConnection(t)
	defer offerer.Close()
	if _, err := offerer.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
		t.Fatalf("add audio transceiver: %v", err)
	}
	offer, err := offerer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	if err := sess.PeerConnection.SetRemoteDescription(offer); err != nil {
		t.Fatalf("SetRemoteDescription on lazy PC failed: %v", err)
	}
	answer, err := sess.PeerConnection.CreateAnswer(nil)
	if err != nil {
		t.Fatalf("CreateAnswer on lazy PC failed: %v", err)
	}
	if err := sess.PeerConnection.SetLocalDescription(answer); err != nil {
		t.Fatalf("SetLocalDescription on lazy PC failed: %v", err)
	}
}

func TestEnsurePeerConnectionRefusesClosedOrEndedSession(t *testing.T) {
	cfg := &config.Config{}

	t.Run("ended_state", func(t *testing.T) {
		sess, err := NewIncomingSession("lazy-ended", cfg, config.TURNConfig{})
		if err != nil {
			t.Fatalf("NewIncomingSession failed: %v", err)
		}
		sess.SetState(StateEnded)
		if !sess.IsClosed() {
			t.Fatalf("expected ended session to report IsClosed() == true")
		}
		if err := sess.EnsurePeerConnection(); err == nil {
			t.Fatalf("expected EnsurePeerConnection to fail on ended session")
		}
		if sess.HasPeerConnection() || sess.HasAudioTrack() || sess.HasVideoTrack() {
			t.Fatalf("expected no PeerConnection or tracks to be leaked on ended session")
		}
	})

	t.Run("closed_session", func(t *testing.T) {
		sess, err := NewIncomingSession("lazy-closed", cfg, config.TURNConfig{})
		if err != nil {
			t.Fatalf("NewIncomingSession failed: %v", err)
		}
		sess.Close()
		if !sess.IsClosed() {
			t.Fatalf("expected closed session to report IsClosed() == true")
		}
		if err := sess.EnsurePeerConnection(); err == nil {
			t.Fatalf("expected EnsurePeerConnection to fail on closed session")
		}
		if sess.HasPeerConnection() || sess.HasAudioTrack() || sess.HasVideoTrack() {
			t.Fatalf("expected no PeerConnection or tracks to be leaked on closed session")
		}
	})

	t.Run("deleted_from_manager", func(t *testing.T) {
		mgr := NewManager(cfg)
		sess, err := mgr.CreateIncomingSession(config.TURNConfig{})
		if err != nil {
			t.Fatalf("CreateIncomingSession failed: %v", err)
		}
		mgr.DeleteSession(sess.ID)
		if err := sess.EnsurePeerConnection(); err == nil {
			t.Fatalf("expected EnsurePeerConnection to fail after DeleteSession")
		}
		if sess.HasPeerConnection() {
			t.Fatalf("expected PeerConnection to remain nil after DeleteSession")
		}
	})

	t.Run("detached_session", func(t *testing.T) {
		sess, err := NewIncomingSession("lazy-detached", cfg, config.TURNConfig{})
		if err != nil {
			t.Fatalf("NewIncomingSession failed: %v", err)
		}
		if pc := sess.DetachPeerConnection(); pc != nil {
			t.Fatalf("expected nil PC from DetachPeerConnection on lazy session")
		}
		if err := sess.EnsurePeerConnection(); err == nil {
			t.Fatalf("expected EnsurePeerConnection to fail on detached session")
		}
		if sess.HasPeerConnection() {
			t.Fatalf("expected PeerConnection to remain nil on detached session")
		}
	})

	t.Run("nil_session", func(t *testing.T) {
		var sess *Session
		if !sess.IsClosed() {
			t.Fatalf("expected nil session to report IsClosed() == true")
		}
		if err := sess.EnsurePeerConnection(); err == nil {
			t.Fatalf("expected EnsurePeerConnection on nil session to return error")
		}
	})
}

func TestLazyIncomingSessionNilPeerConnectionSafety(t *testing.T) {
	cfg := &config.Config{}
	mgr := NewManager(cfg)

	sess, err := mgr.CreateIncomingSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("CreateIncomingSession failed: %v", err)
	}
	sess.SetCallInfo("inbound", "sip:caller@example.com", "sip:callee@example.com", "call-id-lazy-nil")
	sess.SetState(StateIncoming)

	// Metadata & observability helpers
	snap := sess.Snapshot()
	if snap.ID != sess.ID || snap.State != StateIncoming || snap.SIPCallID != "call-id-lazy-nil" {
		t.Fatalf("unexpected Snapshot on lazy session: %+v", snap)
	}
	obs := sess.Observability()
	if obs.SessionID != sess.ID {
		t.Fatalf("unexpected Observability on lazy session: %+v", obs)
	}

	// Feedback / keyframe helpers must be safe no-ops without a PeerConnection
	sess.SendPLItoWebRTC()
	sess.SendFIRToWebRTC()
	_ = sess.KickUplinkKeyframeOnRemoteJoinIfNeeded()
	_ = sess.KickUplinkKeyframeOnFirstSIPRTCPIfNeeded()

	// ICE helpers must not panic when PeerConnection == nil
	if queued, err := sess.AddRemoteICECandidate(webrtc.ICECandidateInit{
		Candidate: "candidate:1 1 udp 2122260223 127.0.0.1 54321 typ host",
	}); err == nil || queued {
		t.Fatalf("expected AddRemoteICECandidate to return error when PC is nil, got queued=%v err=%v", queued, err)
	}
	if flushed := sess.FlushPendingRemoteICE(); flushed != 0 {
		t.Fatalf("expected FlushPendingRemoteICE to return 0 when PC is nil, got %d", flushed)
	}
	if ok := WaitForOfferICEGatheringComplete(context.Background(), nil, false, false, 10*time.Millisecond); ok {
		t.Fatalf("expected WaitForOfferICEGatheringComplete(nil) to return false")
	}
	logICECandidatePairDiagnostics(sess.ID, nil, "lazy-test")

	// SDP / answer helpers must return errors rather than panic when PC is nil
	if _, err := sess.AnswerClientOffer("v=0\r\n"); err == nil {
		t.Fatalf("expected AnswerClientOffer to return error when PC is nil")
	}
	videoOfferSDP := "v=0\r\no=- 1 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=rtpmap:96 H264/90000\r\na=fmtp:96 packetization-mode=1\r\n"
	if err := PreferWebRTCH264PacketizationMode(nil, videoOfferSDP, 1); err == nil {
		t.Fatalf("expected PreferWebRTCH264PacketizationMode(nil, videoSDP, 1) to return error")
	}

	// Media writes must fail cleanly without panicking when tracks are nil
	if err := sess.WriteAudioToWebRTC([]byte{0x80, 0x6f, 0x00, 0x01}); err == nil {
		t.Fatalf("expected WriteAudioToWebRTC to return error when AudioTrack is nil")
	}
	if _, err := sess.WriteVideoToWebRTC([]byte{0x80, 0x60, 0x00, 0x01}); err == nil {
		t.Fatalf("expected WriteVideoToWebRTC to return error when VideoTrack is nil")
	}

	// Close (twice) and DeleteSession must never panic on a lazy session
	sess.Close()
	sess.Close()
	mgr.DeleteSession(sess.ID)
	mgr.DeleteSession(sess.ID)

	// Also verify DeleteSession directly on a never-initialized lazy session
	sess2, err := mgr.CreateIncomingSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("second CreateIncomingSession failed: %v", err)
	}
	mgr.DeleteSession(sess2.ID)
	if !sess2.IsClosed() {
		t.Fatalf("expected DeleteSession to mark lazy session closed")
	}
}

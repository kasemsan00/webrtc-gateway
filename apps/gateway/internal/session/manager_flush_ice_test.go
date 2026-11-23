package session

import (
	"sync"
	"testing"

	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
)

func TestManagerCreateSessionConcurrent(t *testing.T) {
	cfg := &config.Config{}
	mgr := NewManager(cfg)

	const workers = 4
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	ids := make(chan string, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess, err := mgr.CreateSession(config.TURNConfig{})
			if err != nil {
				errs <- err
				return
			}
			ids <- sess.ID
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)

	for err := range errs {
		t.Fatalf("CreateSession failed: %v", err)
	}
	seen := make(map[string]bool)
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate session ID generated: %s", id)
		}
		seen[id] = true
		if _, ok := mgr.GetSession(id); !ok {
			t.Fatalf("session %s missing from manager", id)
		}
		mgr.DeleteSession(id)
	}
	if len(seen) != workers {
		t.Fatalf("created %d sessions, want %d", len(seen), workers)
	}
}

func TestSessionFlushPendingRemoteICE(t *testing.T) {
	offerer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("new offerer pc: %v", err)
	}
	defer offerer.Close()
	if _, err := offerer.CreateDataChannel("test", nil); err != nil {
		t.Fatalf("create data channel: %v", err)
	}
	offer, err := offerer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	if err := offerer.SetLocalDescription(offer); err != nil {
		t.Fatalf("offerer SetLocalDescription: %v", err)
	}
	<-webrtc.GatheringCompletePromise(offerer)

	answerer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("new answerer pc: %v", err)
	}
	defer answerer.Close()

	sess := &Session{
		ID:             "flush-ice",
		PeerConnection: answerer,
	}

	cand := webrtc.ICECandidateInit{
		Candidate: "candidate:1 1 udp 2122260223 127.0.0.1 54321 typ host",
	}
	queued, err := sess.AddRemoteICECandidate(cand)
	if err != nil || !queued {
		t.Fatalf("expected early candidate to be queued, got queued=%v err=%v", queued, err)
	}

	// Flushing before SetRemoteDescription must be a no-op and keep queued candidates.
	if flushed := sess.FlushPendingRemoteICE(); flushed != 0 {
		t.Fatalf("expected 0 flushed before SetRemoteDescription, got %d", flushed)
	}

	if err := answerer.SetRemoteDescription(*offerer.LocalDescription()); err != nil {
		t.Fatalf("answerer SetRemoteDescription: %v", err)
	}

	if flushed := sess.FlushPendingRemoteICE(); flushed != 1 {
		t.Fatalf("expected 1 flushed after SetRemoteDescription, got %d", flushed)
	}
	if flushed := sess.FlushPendingRemoteICE(); flushed != 0 {
		t.Fatalf("expected 0 flushed on second call, got %d", flushed)
	}
}

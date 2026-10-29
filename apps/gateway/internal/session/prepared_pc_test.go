package session

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
)

func newPrepareTestSession(t *testing.T, id string) *Session {
	t.Helper()
	sess, err := NewSession(id, &config.Config{RTP: config.RTPConfig{BufferSize: 4096}}, config.TURNConfig{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	sess.SetState(StateIncoming)
	return sess
}

type closeRecorder struct {
	mu      sync.Mutex
	reasons map[string][]string
}

func (r *closeRecorder) fn(owner string) PreparedCloseFunc {
	return func(reason string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.reasons == nil {
			r.reasons = make(map[string][]string)
		}
		r.reasons[owner] = append(r.reasons[owner], reason)
	}
}

func (r *closeRecorder) get(owner string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.reasons[owner]...)
}

// addUnnegotiatedPrepared registers a prepared PC without an offer. Races only
// exercise the claim/close bookkeeping, so ICE negotiation is not needed.
func addUnnegotiatedPrepared(t *testing.T, sess *Session, owner string, ttl time.Duration, onClosed PreparedCloseFunc) *preparedPeerConnection {
	t.Helper()
	entry, audioSender, videoSender, err := sess.newPreparedPeerConnection(owner, config.TURNConfig{})
	if err != nil {
		t.Fatalf("newPreparedPeerConnection: %v", err)
	}
	entry.onClosed = onClosed
	sess.wirePeerConnection(entry.pc, audioSender, videoSender, false, entry)
	if err := sess.registerPreparedPeerConnection(entry); err != nil {
		t.Fatalf("register prepared: %v", err)
	}
	sess.armPreparedTTL(entry, ttl)
	return entry
}

func waitPeerConnectionClosed(t *testing.T, pc *webrtc.PeerConnection, label string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pc.ConnectionState() == webrtc.PeerConnectionStateClosed {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s: PeerConnection leaked (state=%s)", label, pc.ConnectionState())
}

// (a) A caller CANCEL and an accept race on the same ringing call. Exactly one
// wins, never both, and no prepared PC outlives the race.
func TestPreparedIncomingCancelAcceptRace(t *testing.T) {
	const iterations = 500
	for i := 0; i < iterations; i++ {
		sess := newPrepareTestSession(t, fmt.Sprintf("race-%d", i))
		rec := &closeRecorder{}
		entries := []*preparedPeerConnection{
			addUnnegotiatedPrepared(t, sess, "agent-a", 0, rec.fn("agent-a")),
			addUnnegotiatedPrepared(t, sess, "agent-b", 0, rec.fn("agent-b")),
		}
		originalPC := sess.PeerConnection

		var answered, cancelled atomic.Bool
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if _, err := sess.AcceptPreparedIncoming("agent-a"); err == nil {
				answered.Store(true)
				// What AcceptCall + completeIncomingAccept do on success.
				sess.SetState(StateActive)
				sess.ClearTerminalAction()
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			if sess.GetState() != StateIncoming {
				return
			}
			if !sess.TryBeginIncomingTerminalAction("caller_cancel") {
				return
			}
			cancelled.Store(true)
			sess.ClosePreparedIncoming(PreparedCloseCallerCancelled)
			sess.SetState(StateEnded)
		}()
		close(start)
		wg.Wait()

		if answered.Load() == cancelled.Load() {
			t.Fatalf("iteration %d: answered=%v cancelled=%v, want exactly one winner", i, answered.Load(), cancelled.Load())
		}
		// A late cancel must never end an answered call.
		if answered.Load() {
			if sess.TryBeginIncomingTerminalAction("caller_cancel") {
				t.Fatalf("iteration %d: cancel claimed an answered call", i)
			}
			if sess.GetState() != StateActive {
				t.Fatalf("iteration %d: answered call state=%s", i, sess.GetState())
			}
		}
		if n := sess.PreparedPeerConnectionCount(); n != 0 {
			t.Fatalf("iteration %d: %d prepared PCs left", i, n)
		}
		for _, entry := range entries {
			if answered.Load() && entry.owner == "agent-a" {
				if sess.PeerConnection != entry.pc {
					t.Fatalf("iteration %d: winner PC not installed", i)
				}
				if got := rec.get(entry.owner); len(got) != 0 {
					t.Fatalf("iteration %d: winner was notified %v", i, got)
				}
				continue
			}
			waitPeerConnectionClosed(t, entry.pc, fmt.Sprintf("iteration %d owner %s", i, entry.owner))
			want := PreparedCloseCallerCancelled
			if answered.Load() {
				want = PreparedCloseAnsweredElsewhere
			}
			if got := rec.get(entry.owner); len(got) != 1 || got[0] != want {
				t.Fatalf("iteration %d: owner %s notified %v, want [%s]", i, entry.owner, got, want)
			}
		}
		if answered.Load() {
			waitPeerConnectionClosed(t, originalPC, fmt.Sprintf("iteration %d replaced incoming PC", i))
		}
		sess.SetState(StateEnded)
		ClosePeerConnectionAsync(sess.DetachPeerConnection(), sess.ID)
	}
}

// (b) N agents accept concurrently: exactly one winner, the rest see
// answered-elsewhere and have their PCs closed and are notified once.
func TestPreparedIncomingConcurrentAcceptsSingleWinner(t *testing.T) {
	const agents = 6
	for iter := 0; iter < 20; iter++ {
		sess := newPrepareTestSession(t, fmt.Sprintf("multi-%d", iter))
		rec := &closeRecorder{}
		entries := make([]*preparedPeerConnection, agents)
		for i := range entries {
			owner := fmt.Sprintf("agent-%d", i)
			entries[i] = addUnnegotiatedPrepared(t, sess, owner, 0, rec.fn(owner))
		}

		errs := make([]error, agents)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range entries {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				_, errs[i] = sess.AcceptPreparedIncoming(entries[i].owner)
			}(i)
		}
		close(start)
		wg.Wait()

		winner := -1
		for i, err := range errs {
			if err == nil {
				if winner >= 0 {
					t.Fatalf("iteration %d: two winners %d and %d", iter, winner, i)
				}
				winner = i
				continue
			}
			if !errors.Is(err, ErrPrepareAnsweredElsewhere) {
				t.Fatalf("iteration %d: loser %d err=%v, want answered elsewhere", iter, i, err)
			}
		}
		if winner < 0 {
			t.Fatalf("iteration %d: no winner", iter)
		}
		if got := sess.IncomingClaimOwner(); got != entries[winner].owner {
			t.Fatalf("iteration %d: claim owner %q, want %q", iter, got, entries[winner].owner)
		}
		if sess.PeerConnection != entries[winner].pc {
			t.Fatalf("iteration %d: winner PC not installed", iter)
		}
		for i, entry := range entries {
			if i == winner {
				continue
			}
			waitPeerConnectionClosed(t, entry.pc, fmt.Sprintf("iteration %d loser %d", iter, i))
			if got := rec.get(entry.owner); len(got) != 1 || got[0] != PreparedCloseAnsweredElsewhere {
				t.Fatalf("iteration %d: loser %d notified %v", iter, i, got)
			}
		}
		if n := sess.PreparedPeerConnectionCount(); n != 0 {
			t.Fatalf("iteration %d: %d prepared PCs left", iter, n)
		}
		sess.SetState(StateEnded)
		ClosePeerConnectionAsync(sess.DetachPeerConnection(), sess.ID)
	}
}

// (d) An unclaimed prepared PC is closed after its TTL. The call keeps ringing
// and a later accept from that owner fails instead of claiming the call.
func TestPreparedIncomingTTLExpiry(t *testing.T) {
	sess := newPrepareTestSession(t, "ttl")
	defer func() {
		sess.SetState(StateEnded)
		ClosePeerConnectionAsync(sess.DetachPeerConnection(), sess.ID)
	}()
	rec := &closeRecorder{}
	entry := addUnnegotiatedPrepared(t, sess, "agent-ttl", 50*time.Millisecond, rec.fn("agent-ttl"))
	keep := addUnnegotiatedPrepared(t, sess, "agent-keep", 0, rec.fn("agent-keep"))

	waitPeerConnectionClosed(t, entry.pc, "expired prepared PC")
	if sess.HasPreparedPeerConnection("agent-ttl") {
		t.Fatal("expired prepared PC still registered")
	}
	if reason, ok := sess.PreparedClosedReason("agent-ttl"); !ok || reason != preparedClosedExpired {
		t.Fatalf("closed reason=%q ok=%v, want expired", reason, ok)
	}
	if sess.GetState() != StateIncoming {
		t.Fatalf("TTL changed call state to %s", sess.GetState())
	}
	if !sess.HasPreparedPeerConnection("agent-keep") || keep.pc.ConnectionState() == webrtc.PeerConnectionStateClosed {
		t.Fatal("TTL of one owner closed another owner's prepared PC")
	}
	if _, err := sess.AcceptPreparedIncoming("agent-ttl"); !errors.Is(err, ErrPreparedNotFound) {
		t.Fatalf("accept after TTL err=%v, want ErrPreparedNotFound", err)
	}
	if sess.IncomingClaimOwner() != "" {
		t.Fatal("accept after TTL claimed the call")
	}
}

// Leaving StateIncoming through any SetState closes prepared PCs (safety net).
func TestPreparedIncomingClosedOnStateExit(t *testing.T) {
	sess := newPrepareTestSession(t, "state-exit")
	rec := &closeRecorder{}
	entry := addUnnegotiatedPrepared(t, sess, "agent-x", 0, rec.fn("agent-x"))
	if !sess.TryBeginIncomingTerminalAction("timeout") {
		t.Fatal("expected timeout to claim the ringing call")
	}
	sess.SetState(StateEnded)
	waitPeerConnectionClosed(t, entry.pc, "prepared PC after ring timeout")
	if got := rec.get("agent-x"); len(got) != 1 || got[0] != PreparedCloseNoAnswer {
		t.Fatalf("notified %v, want [no_answer]", got)
	}
	if _, err := sess.PrepareIncomingAnswer("agent-y", "v=0", config.TURNConfig{}, false, 0, nil); !errors.Is(err, ErrPrepareNotIncoming) {
		t.Fatalf("prepare after end err=%v, want ErrPrepareNotIncoming", err)
	}
	ClosePeerConnectionAsync(sess.DetachPeerConnection(), sess.ID)
}

// (f) Before accept, SIP media written to the session tracks never reaches the
// agent and agent media never reaches the SIP forwarder. After accept both do.
func TestPreparedIncomingNoMediaBeforeAccept(t *testing.T) {
	sess := newPrepareTestSession(t, "media-gate")
	defer func() {
		sess.SetState(StateEnded)
		ClosePeerConnectionAsync(sess.DetachPeerConnection(), sess.ID)
	}()
	sess.SetSIPVideoPacketizationMode(1)

	client := newSwitchClientOfferPC(t)
	defer client.Close()
	clientTracks := map[string]*webrtc.TrackLocalStaticRTP{}
	for _, sender := range client.GetSenders() {
		if track, ok := sender.Track().(*webrtc.TrackLocalStaticRTP); ok {
			clientTracks[track.Kind().String()] = track
		}
	}
	var clientRTP atomic.Int64
	client.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		buf := make([]byte, 1500)
		for {
			if _, _, err := track.Read(buf); err != nil {
				return
			}
			clientRTP.Add(1)
		}
	})
	connected := make(chan struct{})
	var connectedOnce sync.Once
	client.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateConnected {
			connectedOnce.Do(func() { close(connected) })
		}
	})
	gather := webrtc.GatheringCompletePromise(client)
	<-gather

	answer, err := sess.PrepareIncomingAnswer("agent-media", client.LocalDescription().SDP, config.TURNConfig{}, false, time.Minute, nil)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := client.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		t.Fatalf("client SetRemoteDescription: %v", err)
	}
	select {
	case <-connected:
	case <-time.After(10 * time.Second):
		t.Skip("no host interface usable for in-process ICE")
	}

	sess.mu.RLock()
	entry := sess.preparedPCs["agent-media"]
	sessionAudio := sess.AudioTrack
	sessionVideo := sess.VideoTrack
	sess.mu.RUnlock()
	if entry == nil {
		t.Fatal("prepared PC missing")
	}
	if sessionAudio == entry.audioTrack || sessionVideo == entry.videoTrack {
		t.Fatal("prepared PC tracks are the session tracks before accept")
	}

	pump := func(d time.Duration) {
		deadline := time.Now().Add(d)
		seq := uint16(0)
		for time.Now().Before(deadline) {
			seq++
			for kind, track := range clientTracks {
				pt := uint8(111)
				if kind == "video" {
					pt = 102
				}
				_ = track.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: pt, SequenceNumber: seq, Timestamp: uint32(seq) * 960, SSRC: 1}, Payload: []byte{0x01, 0x02, 0x03}})
			}
			// SIP→WebRTC writes go to the session tracks.
			_ = sessionAudio.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 111, SequenceNumber: seq, Timestamp: uint32(seq) * 960, SSRC: 7}, Payload: []byte{0xf8, 0xff, 0xfe}})
			time.Sleep(10 * time.Millisecond)
		}
	}
	pump(time.Second)

	if entry.heldPackets.Load() == 0 {
		t.Fatal("expected agent RTP to arrive and be held on the prepared PC")
	}
	if n := clientRTP.Load(); n != 0 {
		t.Fatalf("agent received %d SIP RTP packets before accept", n)
	}
	if sess.mediaForwardReadyForTest() || sess.uplinkVideoPacketSeenForTest() {
		t.Fatal("agent media reached the SIP forwarder before accept")
	}

	if _, err := sess.AcceptPreparedIncoming("agent-media"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	sess.mu.RLock()
	sessionAudio = sess.AudioTrack
	sess.mu.RUnlock()
	if sessionAudio != entry.audioTrack {
		t.Fatal("accept did not install the prepared audio track")
	}
	deadline := time.Now().Add(5 * time.Second)
	for clientRTP.Load() == 0 && time.Now().Before(deadline) {
		pump(100 * time.Millisecond)
	}
	if clientRTP.Load() == 0 {
		t.Fatal("agent received no SIP RTP after accept")
	}
}

func (s *Session) mediaForwardReadyForTest() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mediaForwardReady
}

func (s *Session) uplinkVideoPacketSeenForTest() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.uplinkVideoPacketSeen
}

package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

// prepareTestSIPCallMaker is safe for concurrent accept/cancel races.
type prepareTestSIPCallMaker struct {
	incomingTestSIPCallMaker
	accepts atomic.Int32
	rejects atomic.Int32
}

func (m *prepareTestSIPCallMaker) AcceptCall(sess *session.Session) error {
	m.accepts.Add(1)
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:00025@203.0.113.10:5060>", "203.0.113.20", 5060, 1, nil)
	sess.UpdateState(session.StateActive)
	return nil
}

func (m *prepareTestSIPCallMaker) RejectCall(sess *session.Session, reason string) error {
	m.rejects.Add(1)
	return nil
}

func (m *prepareTestSIPCallMaker) RestartPeriodicPLI(sess *session.Session) {}

const prepareTestTrunkID = 7

func newPrepareTestServer(t *testing.T, ttl time.Duration) (*Server, *session.Manager, *prepareTestSIPCallMaker, *session.Session) {
	t.Helper()
	mgr := newTestSessionManager()
	sipMaker := &prepareTestSIPCallMaker{}
	srv := NewServer(config.APIConfig{WebRTCPrepareTTL: ttl}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, mgr, sipMaker, nil, nil, nil)
	return srv, mgr, sipMaker, newPrepareTestIncoming(t, mgr)
}

func newPrepareTestIncoming(t *testing.T, mgr *session.Manager) *session.Session {
	t.Helper()
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("create incoming session: %v", err)
	}
	sess.SetCallInfo("inbound", "1001", "00025", "sip-call-prepare")
	sess.SetSIPAuthContext("trunk", "", prepareTestTrunkID, "sip.example.test", "00025", "secret", 5060)
	sess.SetSIPVideoPacketizationMode(1)
	sess.SetState(session.StateIncoming)
	t.Cleanup(func() { mgr.DeleteSession(sess.ID) })
	return sess
}

func newPresentedClient(srv *Server, sessionID string) *WSClient {
	client := &WSClient{
		clientID:        fmt.Sprintf("client-%p", new(int)),
		trunkResolved:   true,
		resolvedTrunkID: prepareTestTrunkID,
		agentOnly:       true,
		multiCall:       true,
		send:            make(chan []byte, 64),
		ownedSessionIDs: make(map[string]struct{}),
		pendingIncoming: make(map[string]struct{}),
	}
	srv.mu.Lock()
	srv.wsConnections[client] = struct{}{}
	srv.mu.Unlock()
	srv.markPendingIncoming(client, sessionID)
	return client
}

// newBrowserOffer returns a client PeerConnection with audio+video and a
// complete (non-trickle) offer.
func newBrowserOffer(t *testing.T) (*webrtc.PeerConnection, string) {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("client PeerConnection: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	for _, kind := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeAudio, webrtc.RTPCodecTypeVideo} {
		if _, err := pc.AddTransceiverFromKind(kind, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv}); err != nil {
			t.Fatalf("add transceiver: %v", err)
		}
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("create offer: %v", err)
	}
	gather := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatalf("set local offer: %v", err)
	}
	<-gather
	return pc, pc.LocalDescription().SDP
}

func prepareClient(t *testing.T, srv *Server, sess *session.Session, client *WSClient) {
	t.Helper()
	_, sdp := newBrowserOffer(t)
	srv.handleWSMessage(client, mustJSON(t, WSMessage{Type: "offer", SessionID: sess.ID, SDP: sdp, Prepare: true}))
	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "answer" || msgs[0].SessionID != sess.ID || !msgs[0].Prepare || !strings.Contains(msgs[0].SDP, "a=fingerprint") {
		t.Fatalf("expected prepared answer for %s, got %+v", sess.ID, msgs)
	}
}

func mustJSON(t *testing.T, msg WSMessage) []byte {
	t.Helper()
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func countMessages(msgs []WSMessage, typ, value string) int {
	n := 0
	for _, msg := range msgs {
		if msg.Type != typ {
			continue
		}
		if (typ == "state" && msg.State == value) || (typ == "cancel" && msg.Reason == value) || (typ != "state" && typ != "cancel") {
			n++
		}
	}
	return n
}

// simulateCallerCancel mirrors sip.applyIncomingCallerCancel.
func simulateCallerCancel(srv *Server, mgr *session.Manager, sess *session.Session) bool {
	if sess.GetState() != session.StateIncoming {
		return false
	}
	if !sess.TryBeginIncomingTerminalAction("caller_cancel") {
		return false
	}
	srv.NotifyIncomingCancel(sess.ID, prepareTestTrunkID, "caller_cancelled")
	sess.UpdateState(session.StateEnded)
	mgr.DeleteSession(sess.ID)
	return true
}

func TestWSPrepareOfferDoesNotClaimBindOrAnswerSIP(t *testing.T) {
	srv, _, sipMaker, sess := newPrepareTestServer(t, 0)
	a := newPresentedClient(srv, sess.ID)
	b := newPresentedClient(srv, sess.ID)
	prepareClient(t, srv, sess, a)
	prepareClient(t, srv, sess, b)

	if owner := sess.IncomingClaimOwner(); owner != "" {
		t.Fatalf("prepare claimed the call for %q", owner)
	}
	if sess.GetState() != session.StateIncoming || sipMaker.accepts.Load() != 0 {
		t.Fatalf("prepare changed the call: state=%s accepts=%d", sess.GetState(), sipMaker.accepts.Load())
	}
	if srv.clientOwnsSession(a, sess.ID) || srv.clientOwnsSession(b, sess.ID) {
		t.Fatal("prepare bound the client to the incoming session")
	}
	if n := sess.PreparedPeerConnectionCount(); n != 2 {
		t.Fatalf("expected two prepared PCs, got %d", n)
	}

	// Trickle ICE for a prepared PC is routed to it and allowed on /ws-agent.
	if ok, reason := srv.allowAgentWSMessage(a, WSMessage{Type: "ice", SessionID: sess.ID}); !ok {
		t.Fatalf("ice for prepared PC rejected: %s", reason)
	}
	other := newPresentedClient(srv, sess.ID)
	if ok, _ := srv.allowAgentWSMessage(other, WSMessage{Type: "ice", SessionID: sess.ID}); ok {
		t.Fatal("ice allowed for a presented client without a prepared PC")
	}
	candidate := json.RawMessage(`{"candidate":"candidate:1 1 UDP 2122260223 192.0.2.1 9 typ host","sdpMid":"0"}`)
	srv.handleWSMessage(a, mustJSON(t, WSMessage{Type: "ice", SessionID: sess.ID, Candidate: candidate}))
	if msgs := readWSMessages(t, a.send); len(msgs) != 0 {
		t.Fatalf("prepared ICE produced %+v", msgs)
	}
}

// (a) Caller CANCEL races accept from a prepared client many times.
func TestWSPrepareAcceptCancelRace(t *testing.T) {
	const iterations = 25
	for i := 0; i < iterations; i++ {
		srv, mgr, sipMaker, sess := newPrepareTestServer(t, 0)
		a := newPresentedClient(srv, sess.ID)
		b := newPresentedClient(srv, sess.ID)
		prepareClient(t, srv, sess, a)
		prepareClient(t, srv, sess, b)

		var cancelled atomic.Bool
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			srv.handleWSAccept(a, WSMessage{Type: "accept", SessionID: sess.ID})
		}()
		go func() {
			defer wg.Done()
			<-start
			cancelled.Store(simulateCallerCancel(srv, mgr, sess))
		}()
		close(start)
		wg.Wait()

		answered := sipMaker.accepts.Load() == 1
		if answered == cancelled.Load() || sipMaker.accepts.Load() > 1 {
			t.Fatalf("iteration %d: accepts=%d cancelled=%v, want exactly one winner", i, sipMaker.accepts.Load(), cancelled.Load())
		}
		// Late cancel after an answer must be a no-op.
		if answered && simulateCallerCancel(srv, mgr, sess) {
			t.Fatalf("iteration %d: cancel ended an answered call", i)
		}
		msgsA := readWSMessages(t, a.send)
		msgsB := readWSMessages(t, b.send)
		if answered {
			if countMessages(msgsA, "state", "active") != 1 || countMessages(msgsA, "cancel", "") != 0 {
				t.Fatalf("iteration %d: winner got %+v", i, msgsA)
			}
			if sess.GetState() != session.StateActive {
				t.Fatalf("iteration %d: answered call state=%s", i, sess.GetState())
			}
		} else {
			if countMessages(msgsA, "state", "active") != 0 || countMessages(msgsA, "cancel", "caller_cancelled") != 1 {
				t.Fatalf("iteration %d: cancelled preparer got %+v", i, msgsA)
			}
		}
		wantB := "caller_cancelled"
		if answered {
			wantB = incomingCancelAnsweredElsewhere
		}
		if len(msgsB) != 1 || countMessages(msgsB, "cancel", wantB) != 1 {
			t.Fatalf("iteration %d: other preparer got %+v, want one cancel %s", i, msgsB, wantB)
		}
		if n := sess.PreparedPeerConnectionCount(); n != 0 {
			t.Fatalf("iteration %d: %d prepared PCs leaked", i, n)
		}
	}
}

// (b) N agents prepare, then all accept at once.
func TestWSPrepareConcurrentAcceptsSingleWinner(t *testing.T) {
	const agents = 4
	srv, _, sipMaker, sess := newPrepareTestServer(t, 0)
	clients := make([]*WSClient, agents)
	for i := range clients {
		clients[i] = newPresentedClient(srv, sess.ID)
		prepareClient(t, srv, sess, clients[i])
	}
	bystander := newPresentedClient(srv, sess.ID) // presented, never prepared

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, c := range clients {
		wg.Add(1)
		go func(c *WSClient) {
			defer wg.Done()
			<-start
			srv.handleWSAccept(c, WSMessage{Type: "accept", SessionID: sess.ID})
		}(c)
	}
	close(start)
	wg.Wait()

	if got := sipMaker.accepts.Load(); got != 1 {
		t.Fatalf("AcceptCall ran %d times, want 1", got)
	}
	winners := 0
	for i, c := range clients {
		msgs := readWSMessages(t, c.send)
		if countMessages(msgs, "state", "active") == 1 {
			winners++
			if countMessages(msgs, "cancel", "") != 0 {
				t.Fatalf("winner %d got cancel: %+v", i, msgs)
			}
			if !srv.clientOwnsSession(c, sess.ID) || sess.IncomingClaimOwner() != wsClientClaimID(c) {
				t.Fatalf("winner %d does not own the call", i)
			}
			continue
		}
		if countMessages(msgs, "state", "active") != 0 || countMessages(msgs, "cancel", incomingCancelAnsweredElsewhere) == 0 {
			t.Fatalf("loser %d got %+v", i, msgs)
		}
		for _, msg := range msgs {
			if msg.Type == "cancel" && msg.Reason != incomingCancelAnsweredElsewhere {
				t.Fatalf("loser %d got cancel reason %s", i, msg.Reason)
			}
		}
	}
	if winners != 1 {
		t.Fatalf("got %d winners, want 1", winners)
	}
	if msgs := readWSMessages(t, bystander.send); len(msgs) != 1 || countMessages(msgs, "cancel", incomingCancelAnsweredElsewhere) != 1 {
		t.Fatalf("bystander got %+v, want one answered_elsewhere", msgs)
	}
	if n := sess.PreparedPeerConnectionCount(); n != 0 {
		t.Fatalf("%d prepared PCs left after accept", n)
	}
	if sess.GetState() != session.StateActive {
		t.Fatalf("call state=%s, want active", sess.GetState())
	}
}

// (c) A disconnected socket's prepared PC is closed; the call keeps ringing.
func TestWSPrepareDisconnectClosesPreparedPC(t *testing.T) {
	srv, _, sipMaker, sess := newPrepareTestServer(t, 0)
	gone := newPresentedClient(srv, sess.ID)
	stay := newPresentedClient(srv, sess.ID)
	prepareClient(t, srv, sess, gone)
	prepareClient(t, srv, sess, stay)

	srv.detachWSClient(gone)
	srv.dropClientPreparedPeerConnections(gone)

	if sess.HasPreparedPeerConnection(wsClientClaimID(gone)) {
		t.Fatal("prepared PC survived WebSocket disconnect")
	}
	if !sess.HasPreparedPeerConnection(wsClientClaimID(stay)) {
		t.Fatal("disconnect closed another client's prepared PC")
	}
	if sess.GetState() != session.StateIncoming || sipMaker.rejects.Load() != 0 {
		t.Fatalf("disconnect changed the ringing call: state=%s rejects=%d", sess.GetState(), sipMaker.rejects.Load())
	}
	if msgs := readWSMessages(t, gone.send); len(msgs) != 0 {
		t.Fatalf("disconnected client was sent %+v", msgs)
	}

	srv.handleWSAccept(stay, WSMessage{Type: "accept", SessionID: sess.ID})
	if msgs := readWSMessages(t, stay.send); countMessages(msgs, "state", "active") != 1 {
		t.Fatalf("remaining preparer could not accept: %+v", msgs)
	}
}

// (d) An unclaimed prepared PC expires; a later accept from that client fails
// without answering SIP.
func TestWSPrepareTTLExpiry(t *testing.T) {
	srv, _, sipMaker, sess := newPrepareTestServer(t, 150*time.Millisecond)
	client := newPresentedClient(srv, sess.ID)
	prepareClient(t, srv, sess, client)

	deadline := time.Now().Add(3 * time.Second)
	for sess.HasPreparedPeerConnection(wsClientClaimID(client)) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if sess.HasPreparedPeerConnection(wsClientClaimID(client)) {
		t.Fatal("prepared PC did not expire")
	}
	srv.handleWSAccept(client, WSMessage{Type: "accept", SessionID: sess.ID})
	msgs := readWSMessages(t, client.send)
	if len(msgs) != 1 || msgs[0].Type != "error" || !strings.Contains(msgs[0].Error, "prepare=true") {
		t.Fatalf("accept after TTL got %+v", msgs)
	}
	if sipMaker.accepts.Load() != 0 || sess.IncomingClaimOwner() != "" || sess.GetState() != session.StateIncoming {
		t.Fatalf("accept after TTL changed the call: accepts=%d owner=%q state=%s", sipMaker.accepts.Load(), sess.IncomingClaimOwner(), sess.GetState())
	}
}

func TestWSPrepareOfferRequiresPresentedCall(t *testing.T) {
	srv, _, _, sess := newPrepareTestServer(t, 0)
	stranger := &WSClient{send: make(chan []byte, 8), publicOnly: true}
	_, sdp := newBrowserOffer(t)
	srv.handleWSMessage(stranger, mustJSON(t, WSMessage{Type: "offer", SessionID: sess.ID, SDP: sdp, Prepare: true}))
	msgs := readWSMessages(t, stranger.send)
	if len(msgs) != 1 || msgs[0].Type != "error" {
		t.Fatalf("prepare for an unpresented call got %+v", msgs)
	}
	if n := sess.PreparedPeerConnectionCount(); n != 0 {
		t.Fatalf("unpresented client created %d prepared PCs", n)
	}
}

func TestWebRTCPrepareTTLDefault(t *testing.T) {
	srv := NewServer(config.APIConfig{}, config.TURNConfig{}, config.GatewayConfig{}, config.TranslatorConfig{}, newTestSessionManager(), nil, nil, nil, nil)
	if got := srv.webrtcPrepareTTL(); got != config.DefaultWebRTCPrepareTTL {
		t.Fatalf("default prepare TTL=%s", got)
	}
}

// (e) An offer without prepare keeps claiming the call on offer, and other
// clients' prepared PCs are closed with one answered_elsewhere each.
func TestWSLegacyOfferStillClaimsOnOffer(t *testing.T) {
	srv, _, sipMaker, sess := newPrepareTestServer(t, 0)
	preparer := newPresentedClient(srv, sess.ID)
	prepareClient(t, srv, sess, preparer)
	legacy := newPresentedClient(srv, sess.ID)

	_, sdp := newBrowserOffer(t)
	srv.handleWSMessage(legacy, mustJSON(t, WSMessage{Type: "offer", SessionID: sess.ID, SDP: sdp}))
	msgs := readWSMessages(t, legacy.send)
	if len(msgs) != 1 || msgs[0].Type != "answer" || msgs[0].Prepare {
		t.Fatalf("legacy offer got %+v", msgs)
	}
	if sess.IncomingClaimOwner() != wsClientClaimID(legacy) || !srv.clientOwnsSession(legacy, sess.ID) {
		t.Fatal("legacy offer did not claim and bind the incoming call")
	}
	if sess.GetState() != session.StateIncoming || sipMaker.accepts.Load() != 0 {
		t.Fatal("legacy offer must not answer SIP")
	}

	// The preparer lost at offer time; its accept is answered elsewhere.
	srv.handleWSAccept(preparer, WSMessage{Type: "accept", SessionID: sess.ID})
	if msgs := readWSMessages(t, preparer.send); len(msgs) != 1 || countMessages(msgs, "cancel", incomingCancelAnsweredElsewhere) != 1 {
		t.Fatalf("preparer accept after legacy claim got %+v", msgs)
	}

	srv.handleWSAccept(legacy, WSMessage{Type: "accept", SessionID: sess.ID})
	if msgs := readWSMessages(t, legacy.send); len(msgs) != 1 || countMessages(msgs, "state", "active") != 1 || msgs[0].SessionID != sess.ID {
		t.Fatalf("legacy accept got %+v", msgs)
	}
	if sipMaker.accepts.Load() != 1 {
		t.Fatalf("AcceptCall ran %d times", sipMaker.accepts.Load())
	}
	if msgs := readWSMessages(t, preparer.send); len(msgs) != 1 || countMessages(msgs, "cancel", incomingCancelAnsweredElsewhere) != 1 {
		t.Fatalf("preparer got %+v after legacy accept, want exactly one answered_elsewhere", msgs)
	}
	if n := sess.PreparedPeerConnectionCount(); n != 0 {
		t.Fatalf("%d prepared PCs left after legacy accept", n)
	}
}

// Ring timeout closes prepared PCs and notifies each preparer once.
func TestWSPrepareRingTimeoutClosesPreparedPCs(t *testing.T) {
	srv, _, sipMaker, sess := newPrepareTestServer(t, 0)
	srv.config.IncomingRingTimeoutSeconds = 1
	client := newPresentedClient(srv, sess.ID)
	prepareClient(t, srv, sess, client)

	srv.startIncomingRingTimeout(sess.ID, prepareTestTrunkID)
	deadline := time.Now().Add(4 * time.Second)
	for sipMaker.rejects.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if sipMaker.rejects.Load() != 1 {
		t.Fatal("ring timeout did not reject the call")
	}
	if msgs := readWSMessages(t, client.send); len(msgs) != 1 || countMessages(msgs, "cancel", "no_answer") != 1 {
		t.Fatalf("preparer got %+v, want one no_answer cancel", msgs)
	}
	if n := sess.PreparedPeerConnectionCount(); n != 0 {
		t.Fatalf("%d prepared PCs left after ring timeout", n)
	}
}

package sip

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/pion/rtcp"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

// auxPeerNotifier is the API server as the SIP side sees it for a shared
// screen: whether a session's client can receive one, the client
// renegotiation trigger, and stream state changes.
type auxPeerNotifier struct {
	midCallRenegotiationNotifierStub
	mu         sync.Mutex
	canReceive map[string]bool
	offered    []string
	states     []string
	onOffered  func(sessionID string)
}

func (n *auxPeerNotifier) CanReceiveAuxVideo(sessionID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.canReceive[sessionID]
}

func (n *auxPeerNotifier) NotifyAuxVideoOffered(sessionID string) {
	n.mu.Lock()
	n.offered = append(n.offered, sessionID)
	onOffered := n.onOffered
	n.mu.Unlock()
	if onOffered != nil {
		onOffered(sessionID)
	}
}

func (n *auxPeerNotifier) NotifyAuxVideoState(sessionID string, active bool, reason string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.states = append(n.states, fmt.Sprintf("%s:%v:%s", sessionID, active, reason))
}

func (n *auxPeerNotifier) snapshot() (offered, states []string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.offered...), append([]string(nil), n.states...)
}

func (n *auxPeerNotifier) waitForState(t *testing.T, want string) {
	t.Helper()
	waitFor(t, "stream state "+want, func() bool {
		_, states := n.snapshot()
		for _, state := range states {
			if state == want {
				return true
			}
		}
		return false
	})
}

// twoLegRig is one gateway with both legs of a call: the caller's outbound
// session (the sharer) and the agent's inbound session (the receiver), each
// with its own SIP dialog. The test plays Asterisk between them.
type twoLegRig struct {
	srv      *Server
	notifier *auxPeerNotifier
	sharer   *session.Session
	receiver *session.Session
	requests []*sip.Request
	respond  func(req *sip.Request) (*sip.Response, error)
	mu       sync.Mutex
}

const (
	sharerCallID   = "call-leg-sharer"
	receiverCallID = "call-leg-receiver"
)

func newTwoLegRig(t *testing.T) *twoLegRig {
	t.Helper()
	cfg := &config.Config{
		SIP: config.SIPConfig{MidCallRenegotiationEnable: true, Domain: "203.0.113.20", Port: 5060},
		RTP: config.RTPConfig{BufferSize: 2048},
	}
	cfg.RTP.PortMin, cfg.RTP.PortMax = findFreeContiguousUDPBlock(t, 3)
	mgr := session.NewManager(cfg)
	rig := &twoLegRig{notifier: &auxPeerNotifier{canReceive: map[string]bool{}}}
	rig.srv = &Server{
		config:          cfg.SIP,
		rtpConfig:       cfg.RTP,
		sessionMgr:      mgr,
		publicAddress:   "203.0.113.10",
		sipPort:         5090,
		midCallNotifier: rig.notifier,
	}
	rig.srv.glareBackoff = func(*session.Session) time.Duration { return 0 }
	rig.srv.inDialogInvite = func(req *sip.Request) (*sip.Response, error) {
		rig.mu.Lock()
		rig.requests = append(rig.requests, req)
		respond := rig.respond
		rig.mu.Unlock()
		if respond == nil {
			return nil, fmt.Errorf("no SIP peer configured")
		}
		return respond(req)
	}

	newLeg := func(direction, callID, user string, cseq int) *session.Session {
		sess, err := mgr.CreateSession(config.TURNConfig{})
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		t.Cleanup(func() { mgr.DeleteSession(sess.ID) })
		// Both legs carry the caller's number in From: that is how the two
		// sessions of one call are paired.
		sess.SetCallInfo(direction, "sip:1001@203.0.113.20", "sip:7000@203.0.113.20", callID)
		sess.SetSIPDialogState("local-"+user, "remote-"+user, "<sip:pbx@203.0.113.20:5060>", "203.0.113.20", 5060, cseq, nil)
		sess.SetSIPAuthContext("trunk", "", int64(cseq), "203.0.113.20", user, "secret", 5060)
		sess.SetState(session.StateActive)
		sess.SetRTPConnection(nil, 12000)
		sess.SetAsteriskEndpoints(
			&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 20000},
			&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 20002},
		)
		sess.ApplyMidCallSDPValidation(session.MidCallSDPValidation{
			Accept: true, HasActiveVideo: true,
			Audio: session.MidCallMedia{Direction: "sendrecv"},
			Video: session.MidCallMedia{Direction: "sendrecv"},
		})
		return sess
	}
	rig.sharer = newLeg("outbound", sharerCallID, "1001", 10)
	rig.receiver = newLeg("inbound", receiverCallID, "agent7", 20)
	return rig
}

func (r *twoLegRig) requestCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

// asteriskForwards makes the test's Asterisk accept the sharer's re-INVITE
// and offer the same m-lines to the receiving leg, as a two-party bridge does
// when one side adds or closes a stream. It returns the receiving leg's
// answers.
func (r *twoLegRig) asteriskForwards(t *testing.T) *[]string {
	t.Helper()
	var answers []string
	r.respond = func(req *sip.Request) (*sip.Response, error) {
		ports := []int{20000, 20002, 20006}
		if lines := sdpMediaLines(req.Body()); len(lines) == 3 && strings.HasPrefix(lines[2], "m=video 0 ") {
			ports[2] = 0
		}
		res, err := answerMirroring(req, ports...)
		if err != nil {
			return nil, err
		}
		// The offer toward the agent comes from Asterisk's own address and
		// ports; only its shape follows the sharer's.
		forwarded := sdpMediaPort.ReplaceAllStringFunc(string(req.Body()), func(match string) string {
			return match
		})
		forwarded = strings.ReplaceAll(forwarded, "c=IN IP4 203.0.113.10", "c=IN IP4 10.0.0.9")
		tx := &midCallServerTxStub{}
		r.srv.handleINVITE(newMidCallInviteRequest(receiverCallID, forwarded), tx)
		if len(tx.responses) == 2 && tx.responses[1].StatusCode == 200 {
			answers = append(answers, string(tx.responses[1].Body()))
		} else {
			t.Errorf("receiving leg did not answer the forwarded re-INVITE with 200: %d responses", len(tx.responses))
		}
		return res, nil
	}
	return &answers
}

func TestScreenShareAcrossBothLegsIsConfirmedAndClosed(t *testing.T) {
	rig := newTwoLegRig(t)
	rig.notifier.canReceive[rig.receiver.ID] = true
	// The receiving client renegotiates at once and reports ready.
	rig.notifier.onOffered = func(sessionID string) { rig.srv.ReportScreenStreamToPeer(rig.receiver, true) }
	answers := rig.asteriskForwards(t)

	assertOutcome(t, rig.srv.OpenScreenStream(rig.sharer), "ok", "")

	receiving := rig.receiver.AuxVideo()
	if receiving == nil || receiving.SIPDirection() != "recvonly" {
		t.Fatalf("receiving leg must have an open recvonly stream, got %v", receiving)
	}
	sharing := rig.sharer.AuxVideo()
	if sharing == nil || !sharing.PeerConfirmed() {
		t.Fatal("the sharing leg must be confirmed by the receiving leg")
	}
	recvPort, _ := receiving.Ports()
	sharePort, _ := sharing.Ports()
	if recvPort == sharePort {
		t.Fatal("the two legs must use different local ports")
	}
	if len(*answers) != 1 {
		t.Fatalf("receiving leg answers = %d, want 1", len(*answers))
	}
	assertMediaLines(t, []byte((*answers)[0]),
		"m=audio 12000 RTP/AVP 111 101", "m=video 12002 RTP/AVP 96", fmt.Sprintf("m=video %d RTP/AVP 96", recvPort))
	if !strings.HasSuffix((*answers)[0], "a=content:slides\na=recvonly\n") {
		t.Fatalf("the receiving leg must answer the screen m-line recvonly\n%s", (*answers)[0])
	}
	offered, _ := rig.notifier.snapshot()
	if len(offered) != 1 || offered[0] != rig.receiver.ID {
		t.Fatalf("client renegotiation started for %v, want the receiving session", offered)
	}
	rig.notifier.waitForState(t, rig.sharer.ID+":true:")

	// The sharer stops. Asterisk closes the m-line toward the agent too.
	if !rig.srv.CloseScreenStream(rig.sharer, "client-remove") {
		t.Fatal("expected the shared stream to close")
	}
	if rig.receiver.AuxVideo() != nil {
		t.Fatal("the receiving leg's stream must end when the m-line is closed")
	}
	rig.notifier.waitForState(t, rig.receiver.ID+":false:peer_closed")
	if lines := sdpMediaLines([]byte((*answers)[1])); len(lines) != 3 || !strings.HasPrefix(lines[2], "m=video 0 ") {
		t.Fatalf("receiving leg must answer the closed m-line with port 0, got %q", lines)
	}
}

func TestScreenShareToClientWithoutMultiVideoIsClosedOnTheSharingLeg(t *testing.T) {
	rig := newTwoLegRig(t)
	answers := rig.asteriskForwards(t) // the receiving client did not opt in

	assertOutcome(t, rig.srv.OpenScreenStream(rig.sharer), "ok", "")

	if rig.receiver.AuxVideo() != nil {
		t.Fatal("the receiving leg must refuse the stream")
	}
	if lines := sdpMediaLines([]byte((*answers)[0])); len(lines) != 3 || !strings.HasPrefix(lines[2], "m=video 0 ") {
		t.Fatalf("receiving leg must answer port 0, got %q", lines)
	}
	// The refusal is reported straight to the sharing session, which closes
	// its m-line and tells its client, without waiting for the timeout.
	rig.notifier.waitForState(t, rig.sharer.ID+":false:peer_not_supported")
	if rig.sharer.AuxVideo() != nil {
		t.Fatal("the sharing leg's stream must be closed")
	}
	if offered, _ := rig.notifier.snapshot(); len(offered) != 0 {
		t.Fatalf("no client renegotiation expected, got %v", offered)
	}
}

func TestScreenShareIsClosedWhenFarEndStaysSilent(t *testing.T) {
	rig := newTwoLegRig(t)
	rig.srv.screenConfirmTimeout = 60 * time.Millisecond
	// Asterisk accepts on the sharing leg but the far end is not a gateway
	// session that could confirm.
	rig.respond = func(req *sip.Request) (*sip.Response, error) {
		if lines := sdpMediaLines(req.Body()); strings.HasPrefix(lines[2], "m=video 0 ") {
			return answerMirroring(req, 20000, 20002, 0)
		}
		return answerMirroring(req, 20000, 20002, 20006)
	}

	assertOutcome(t, rig.srv.OpenScreenStream(rig.sharer), "ok", "")
	if rig.sharer.AuxVideo() == nil {
		t.Fatal("the stream is open until the confirmation times out")
	}

	rig.notifier.waitForState(t, rig.sharer.ID+":false:peer_not_supported")
	if rig.sharer.AuxVideo() != nil {
		t.Fatal("an unconfirmed stream must be closed")
	}
	if got := rig.requestCount(); got != 2 {
		t.Fatalf("requests = %d, want the add and the closing re-INVITE", got)
	}
	rig.mu.Lock()
	closing := rig.requests[1].Body()
	rig.mu.Unlock()
	if lines := sdpMediaLines(closing); !strings.HasPrefix(lines[2], "m=video 0 ") {
		t.Fatalf("closing re-INVITE m-lines = %q", lines)
	}
	if _, pending := rig.sharer.GetPendingMidCallRenegotiation(); pending {
		t.Fatal("the closing negotiation must release its claim")
	}
}

func TestConfirmedScreenShareSurvivesTheTimeout(t *testing.T) {
	rig := newTwoLegRig(t)
	rig.srv.screenConfirmTimeout = 60 * time.Millisecond
	rig.respond = func(req *sip.Request) (*sip.Response, error) { return answerMirroring(req, 20000, 20002, 20006) }
	assertOutcome(t, rig.srv.OpenScreenStream(rig.sharer), "ok", "")
	var keyframeRequests int
	var mu sync.Mutex
	rig.sharer.AuxVideo().BindUplinkTrack(0xCAFE, func([]rtcp.Packet) error {
		mu.Lock()
		keyframeRequests++
		mu.Unlock()
		return nil
	})

	rig.srv.ConfirmScreenStream(rig.sharer, true)
	rig.srv.ConfirmScreenStream(rig.sharer, true) // a repeated report changes nothing

	time.Sleep(200 * time.Millisecond)
	if rig.sharer.AuxVideo() == nil {
		t.Fatal("a confirmed stream must stay open")
	}
	_, states := rig.notifier.snapshot()
	if want := rig.sharer.ID + ":true:"; len(states) != 1 || states[0] != want {
		t.Fatalf("stream states = %v, want exactly [%s]", states, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if keyframeRequests != 1 {
		t.Fatalf("browser keyframe requests = %d, want 1 so the far end starts with a whole picture", keyframeRequests)
	}
	if got := rig.requestCount(); got != 1 {
		t.Fatalf("requests = %d, want only the add", got)
	}
}

func TestOpenScreenStreamWhileReceivingIsRefused(t *testing.T) {
	rig := newTwoLegRig(t)
	rig.notifier.canReceive[rig.receiver.ID] = true
	tx := &midCallServerTxStub{}
	rig.srv.handleINVITE(newMidCallInviteRequest(receiverCallID, offerWithSecondVideo), tx)
	assertResponseCodes(t, tx, []int{100, 200})
	if aux := rig.receiver.AuxVideo(); aux == nil || aux.SIPDirection() != "recvonly" {
		t.Fatal("expected the receiving stream to be open")
	}
	// The offer's screen m-line uses AVPF and payload type 100.
	auxPort, _ := rig.receiver.AuxVideo().Ports()
	assertMediaLines(t, tx.responses[1].Body(),
		"m=audio 12000 RTP/AVP 111 101", "m=video 12002 RTP/AVP 96", fmt.Sprintf("m=video %d RTP/AVPF 100", auxPort))
	if got := rig.receiver.AuxVideo().RemoteRTPAddr(); got == nil || got.String() != "10.0.0.77:20006" {
		t.Fatalf("auxiliary RTP source = %v, want 10.0.0.77:20006", got)
	}

	assertOutcome(t, rig.srv.OpenScreenStream(rig.receiver), "failed", "peer_sharing")
	if got := rig.requestCount(); got != 0 {
		t.Fatalf("no re-INVITE may be sent, got %d", got)
	}
}

func TestInboundSecondVideoIsRefusedWhenOpeningIsNotAllowed(t *testing.T) {
	rig := newTwoLegRig(t)
	rig.notifier.canReceive[rig.receiver.ID] = true
	base := rig.srv.createSDPAnswerForInvite(12000, rig.receiver, []byte(offerWithSecondVideo))

	// The initial INVITE of a call: a screen share starts after the answer.
	answer, opened := rig.srv.answerSDPForInboundOffer(rig.receiver, base, []byte(offerWithSecondVideo), false)

	if opened || rig.receiver.AuxVideo() != nil {
		t.Fatal("no stream may be opened")
	}
	if lines := sdpMediaLines(answer); len(lines) != 3 || !strings.HasPrefix(lines[2], "m=video 0 ") {
		t.Fatalf("m-lines = %q, want the third refused", lines)
	}

	// A stream the far end marks recvonly carries nothing toward us.
	recvonly := strings.Replace(offerWithSecondVideo, "a=content:slides\r\na=sendonly", "a=content:slides\r\na=recvonly", 1)
	if _, opened := rig.srv.answerSDPForInboundOffer(rig.receiver, base, []byte(recvonly), true); opened {
		t.Fatal("a recvonly screen offer must not open a receiving stream")
	}
}

func TestReceivingLegRelaysKeyframeRequestToSharingLeg(t *testing.T) {
	rig := newTwoLegRig(t)
	rig.notifier.canReceive[rig.receiver.ID] = true
	rig.asteriskForwards(t)
	assertOutcome(t, rig.srv.OpenScreenStream(rig.sharer), "ok", "")
	var requests int
	var mu sync.Mutex
	rig.sharer.AuxVideo().BindUplinkTrack(0xCAFE, func([]rtcp.Packet) error {
		mu.Lock()
		requests++
		mu.Unlock()
		return nil
	})

	// The agent's browser asks for a keyframe on the screen track. The
	// request must reach the sharer's browser even if the PBX drops the RTCP.
	receiving := rig.receiver.AuxVideo()
	receiving.HandleSIPRTP(rtpPacketForTest(1, 0x51515151))
	receiving.HandleBrowserRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: 1}})

	waitFor(t, "the relayed keyframe request", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return requests == 1
	})
}

func rtpPacketForTest(seq uint16, ssrc uint32) []byte {
	return []byte{
		0x80, 99, byte(seq >> 8), byte(seq), 0, 0, 0x23, 0x28,
		byte(ssrc >> 24), byte(ssrc >> 16), byte(ssrc >> 8), byte(ssrc), 0x65, 0x01,
	}
}

func TestScreenControlMessageIsConsumedAndRouted(t *testing.T) {
	rig := newTwoLegRig(t)
	rig.respond = func(req *sip.Request) (*sip.Response, error) { return answerMirroring(req, 20000, 20002, 20006) }
	assertOutcome(t, rig.srv.OpenScreenStream(rig.sharer), "ok", "")
	chat := &messageNotifierCounter{}
	rig.srv.messageNotifier = chat

	// A PBX-routed MESSAGE addressed to the sharing user.
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: "1001", Host: "203.0.113.10", Port: 5090})
	req.AppendHeader(sip.NewHeader("Call-ID", "message-out-of-dialog"))
	req.AppendHeader(sip.NewHeader("From", "<sip:agent7@203.0.113.20>;tag=x"))
	req.AppendHeader(sip.NewHeader("To", "<sip:1001@203.0.113.20>"))
	req.AppendHeader(sip.NewHeader("CSeq", "1 MESSAGE"))
	req.AppendHeader(sip.NewHeader("Content-Type", "text/plain"))
	req.SetBody([]byte(screenStreamReadyBody))
	tx := &midCallServerTxStub{}

	rig.srv.handleMESSAGE(req, tx)

	assertResponseCodes(t, tx, []int{200})
	waitFor(t, "the sharing leg to be confirmed", func() bool { return rig.sharer.AuxVideo().PeerConfirmed() })
	if chat.count() != 0 {
		t.Fatal("a screen control MESSAGE must not be delivered as chat")
	}

	// In-dialog form, refusing: the session is named by Call-ID.
	rig.srv.handleScreenStreamMessage(screenStreamUnsupportedBody, sharerCallID, "")
	if rig.sharer.AuxVideo() != nil {
		t.Fatal("an unsupported report must close the stream")
	}
}

type messageNotifierCounter struct {
	mu    sync.Mutex
	calls int
}

func (m *messageNotifierCounter) NotifySIPMessage(string, string, string, string) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
}

func (m *messageNotifierCounter) NotifySIPMessageOnDialog(string, string, string, string, string) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
}

func (m *messageNotifierCounter) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestScreenControlMessageForAmbiguousUserIsIgnored(t *testing.T) {
	rig := newTwoLegRig(t)
	rig.respond = func(req *sip.Request) (*sip.Response, error) { return answerMirroring(req, 20000, 20002, 20006) }
	assertOutcome(t, rig.srv.OpenScreenStream(rig.sharer), "ok", "")

	rig.srv.handleScreenStreamMessage(screenStreamReadyBody, "", "sip:someone-else@203.0.113.20")
	rig.srv.handleScreenStreamMessage("@screen:what", sharerCallID, "")

	if rig.sharer.AuxVideo().PeerConfirmed() {
		t.Fatal("a report for another user or an unknown control body must not confirm the stream")
	}
}

func TestSwitchClosesAuxiliaryStreamOnEitherSide(t *testing.T) {
	t.Run("sharing", func(t *testing.T) {
		rig := newTwoLegRig(t)
		rig.respond = func(req *sip.Request) (*sip.Response, error) {
			if lines := sdpMediaLines(req.Body()); strings.HasPrefix(lines[2], "m=video 0 ") {
				return answerMirroring(req, 20000, 20002, 0)
			}
			return answerMirroring(req, 20000, 20002, 20006)
		}
		assertOutcome(t, rig.srv.OpenScreenStream(rig.sharer), "ok", "")

		rig.srv.closeAuxVideoForSwitch(rig.sharer)

		if rig.sharer.AuxVideo() != nil {
			t.Fatal("the stream must be closed")
		}
		rig.notifier.waitForState(t, rig.sharer.ID+":false:agent_switch")
		if got := rig.requestCount(); got != 2 {
			t.Fatalf("requests = %d, want the add and the closing re-INVITE", got)
		}
	})
	t.Run("receiving", func(t *testing.T) {
		rig := newTwoLegRig(t)
		rig.notifier.canReceive[rig.receiver.ID] = true
		tx := &midCallServerTxStub{}
		rig.srv.handleINVITE(newMidCallInviteRequest(receiverCallID, offerWithSecondVideo), tx)

		rig.srv.closeAuxVideoForSwitch(rig.receiver)

		if rig.receiver.AuxVideo() != nil {
			t.Fatal("the stream must be closed")
		}
		rig.notifier.waitForState(t, rig.receiver.ID+":false:agent_switch")
	})
	t.Run("no stream", func(t *testing.T) {
		rig := newTwoLegRig(t)
		rig.srv.closeAuxVideoForSwitch(rig.sharer)
		if _, states := rig.notifier.snapshot(); len(states) != 0 {
			t.Fatalf("no state change expected, got %v", states)
		}
	})
}

func TestEndingTheSessionReleasesAuxiliaryPorts(t *testing.T) {
	rig := newTwoLegRig(t)
	rig.respond = func(req *sip.Request) (*sip.Response, error) { return answerMirroring(req, 20000, 20002, 20006) }
	assertOutcome(t, rig.srv.OpenScreenStream(rig.sharer), "ok", "")
	port, _ := rig.sharer.AuxVideo().Ports()

	rig.srv.sessionMgr.DeleteSession(rig.sharer.ID)

	if rig.sharer.AuxVideo() != nil {
		t.Fatal("deleting the session must close the stream")
	}
	waitFor(t, "the auxiliary port to be released", func() bool {
		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
		if err != nil {
			return false
		}
		conn.Close()
		return true
	})
}

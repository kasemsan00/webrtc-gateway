package session

import (
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// negotiateInitialCall connects a gateway PeerConnection and a client that
// sends camera video, with full ICE gathering on both sides.
func negotiateInitialCall(t *testing.T, gateway, client *webrtc.PeerConnection) {
	t.Helper()
	camera, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000}, "camera", "client-camera")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.AddTrack(camera); err != nil {
		t.Fatal(err)
	}
	offer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(client)
	if err := client.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	if err := gateway.SetRemoteDescription(*client.LocalDescription()); err != nil {
		t.Fatal(err)
	}
	answer, err := gateway.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered = webrtc.GatheringCompletePromise(gateway)
	if err := gateway.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	if err := client.SetRemoteDescription(*gateway.LocalDescription()); err != nil {
		t.Fatal(err)
	}
}

func newReceivingSession(t *testing.T) (*Session, *AuxVideo, *webrtc.PeerConnection, *webrtc.PeerConnection) {
	t.Helper()
	gateway := newGatewayH264Answerer(t)
	t.Cleanup(func() { gateway.Close() })
	clientMedia, err := createCustomMediaEngine()
	if err != nil {
		t.Fatal(err)
	}
	client := newLoopbackPeerConnectionWithMediaEngine(t, clientMedia)
	t.Cleanup(func() { client.Close() })
	gateway.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go func() {
			buf := make([]byte, 2048)
			for {
				if _, _, err := track.Read(buf); err != nil {
					return
				}
			}
		}()
	})
	negotiateInitialCall(t, gateway, client)

	sess := &Session{ID: "aux-receive", RTPBufferSize: 2048, State: StateActive, PeerConnection: gateway}
	sess.SetSIPVideoPacketizationMode(1)
	rtpConn := listenLoopbackUDP(t)
	aux, err := sess.OpenAuxVideo(rtpConn, listenLoopbackUDP(t), udpAddrOf(rtpConn).Port, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.CloseAuxVideo("test-cleanup") })
	aux.SetSIPDirection("recvonly")
	aux.SetRemoteEndpoints(udpAddrOf(listenLoopbackUDP(t)), nil, true)
	return sess, aux, gateway, client
}

// The receiving side of a share: the SIP stream is open, the gateway has put
// a send-only transceiver on the live PeerConnection, and the client answers
// stream_added with an offer that adds a receive-only video section. The two
// must pair, and the screen must arrive on the client as a second video track.
func TestStreamAddedOfferPairsWithPreparedTrackAndDeliversScreen(t *testing.T) {
	sess, aux, gateway, client := newReceivingSession(t)

	if sess.HasNegotiatedAuxDownlink() {
		t.Fatal("nothing is negotiated before the client offers")
	}
	if err := sess.PrepareAuxDownlinkTrack(); err != nil {
		t.Fatalf("PrepareAuxDownlinkTrack: %v", err)
	}
	if err := sess.PrepareAuxDownlinkTrack(); err != nil {
		t.Fatalf("PrepareAuxDownlinkTrack is idempotent: %v", err)
	}
	if err := sess.AttachAuxDownlinkTrack(); err == nil {
		t.Fatal("attaching before the client's offer must fail")
	}

	screenTracks := make(chan *webrtc.TrackRemote, 1)
	client.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if track.StreamID() == "pion-screen-"+sess.ID || track.ID() == "screen" {
			screenTracks <- track
		}
	})
	if _, err := client.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	offer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}

	answerSDP, err := sess.AnswerClientOfferInPlace(offer.SDP)
	if err != nil {
		t.Fatalf("in-place renegotiation failed: %v", err)
	}
	if sess.PeerConnection != gateway {
		t.Fatal("the PeerConnection must not be replaced")
	}
	if err := sess.AttachAuxDownlinkTrack(); err != nil {
		t.Fatalf("AttachAuxDownlinkTrack: %v", err)
	}
	if !sess.HasNegotiatedAuxDownlink() {
		t.Fatal("the auxiliary section must be negotiated now")
	}
	if got := strings.Count(answerSDP, "a=sendonly"); got < 1 {
		t.Fatalf("the gateway must answer the new section sendonly:\n%s", answerSDP)
	}
	if err := client.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answerSDP}); err != nil {
		t.Fatalf("client rejected the gateway answer: %v", err)
	}

	// Screen RTP from the SIP side now reaches the client.
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		seq := uint16(100)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				for _, payload := range [][]byte{auxTestSPS, auxTestPPS, auxTestIDR} {
					seq++
					aux.HandleSIPRTP(auxDownlinkPacket(seq, payload))
				}
			}
		}
	}()

	select {
	case track := <-screenTracks:
		buf := make([]byte, 2048)
		if err := track.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if n, _, err := track.Read(buf); err != nil || n == 0 {
			t.Fatalf("client could not read the screen track: n=%d err=%v", n, err)
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("the client never received the screen track; stats=%+v", aux.Stats())
	}

	// A second share on the same call reuses the section: closing the stream
	// keeps the transceiver, so nothing needs renegotiating.
	sess.CloseAuxVideo("share-ended")
	rtpConn := listenLoopbackUDP(t)
	again, err := sess.OpenAuxVideo(rtpConn, listenLoopbackUDP(t), udpAddrOf(rtpConn).Port, 0)
	if err != nil {
		t.Fatal(err)
	}
	again.SetSIPDirection("recvonly")
	if !sess.HasNegotiatedAuxDownlink() {
		t.Fatal("the negotiated section must survive a closed stream")
	}
	if err := sess.AttachAuxDownlinkTrack(); err != nil {
		t.Fatalf("re-attach for a second share: %v", err)
	}
	again.HandleSIPRTP(auxDownlinkPacket(1, auxTestIDR))
	again.HandleSIPRTP(auxDownlinkPacket(2, auxTestP))
	if stats := again.Stats(); stats.DownlinkWritten != 2 {
		t.Fatalf("second share stats = %+v, want two packets written", stats)
	}
}

func TestAttachAuxDownlinkTrackFailsWhenOfferHasNoNewSection(t *testing.T) {
	sess, _, _, client := newReceivingSession(t)
	if err := sess.PrepareAuxDownlinkTrack(); err != nil {
		t.Fatal(err)
	}
	// The client "answers" stream_added with an offer that changes nothing.
	offer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AnswerClientOfferInPlace(offer.SDP); err != nil {
		t.Fatalf("in-place renegotiation failed: %v", err)
	}

	if err := sess.AttachAuxDownlinkTrack(); err == nil {
		t.Fatal("expected an error: the offer has no receive-only video section for the screen")
	}
	if sess.HasNegotiatedAuxDownlink() {
		t.Fatal("nothing was negotiated")
	}
}

func TestDetachingThePeerConnectionDropsTheAuxiliaryTrack(t *testing.T) {
	sess, aux, _, _ := newReceivingSession(t)
	if err := sess.PrepareAuxDownlinkTrack(); err != nil {
		t.Fatal(err)
	}

	sess.detachAuxVideoTrack()

	if err := sess.AttachAuxDownlinkTrack(); err == nil {
		t.Fatal("a track of a replaced PeerConnection must not be attached again")
	}
	aux.HandleSIPRTP(auxDownlinkPacket(1, auxTestIDR))
	if stats := aux.Stats(); stats.DownlinkWritten != 0 {
		t.Fatalf("stats = %+v, want nothing written without a track", stats)
	}
}

func TestPrepareAuxForReplacementOnResume(t *testing.T) {
	const withScreen = "v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=mid:0\r\n" +
		"m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=mid:1\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=mid:5\r\n"
	const cameraOnly = "v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=mid:0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=mid:1\r\n"

	newSharing := func(t *testing.T, direction string) *Session {
		sess := &Session{ID: "aux-resume", RTPBufferSize: 2048, State: StateActive}
		rtpConn := listenLoopbackUDP(t)
		aux, err := sess.OpenAuxVideo(rtpConn, listenLoopbackUDP(t), udpAddrOf(rtpConn).Port, 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { sess.CloseAuxVideo("test-cleanup") })
		aux.SetSIPDirection(direction)
		sess.SetAuxUplinkMid("2")
		return sess
	}

	t.Run("sharing, offer keeps the screen", func(t *testing.T) {
		sess := newSharing(t, "sendonly")
		sess.prepareAuxForReplacement(nil, withScreen)
		if got := sess.AuxUplinkMid(); got != "5" {
			t.Fatalf("auxiliary mid = %q, want the new offer's 5", got)
		}
		if outcome := sess.TakeAuxResumeOutcome(); outcome.Lost || !outcome.Sending {
			t.Fatalf("outcome = %+v, want kept and sending", outcome)
		}
	})
	t.Run("sharing, offer drops the screen", func(t *testing.T) {
		sess := newSharing(t, "sendonly")
		sess.prepareAuxForReplacement(nil, cameraOnly)
		if sess.AuxUplinkMid() != "" {
			t.Fatal("the auxiliary mid must be cleared")
		}
		outcome := sess.TakeAuxResumeOutcome()
		if !outcome.Lost || !outcome.Sending {
			t.Fatalf("outcome = %+v, want lost and sending", outcome)
		}
		if again := sess.TakeAuxResumeOutcome(); again.Lost {
			t.Fatal("the outcome must be reported once")
		}
	})
	t.Run("receiving, offer drops the screen", func(t *testing.T) {
		sess := newSharing(t, "recvonly")
		sess.prepareAuxForReplacement(nil, cameraOnly)
		if outcome := sess.TakeAuxResumeOutcome(); !outcome.Lost || outcome.Sending {
			t.Fatalf("outcome = %+v, want lost and receiving", outcome)
		}
	})
	t.Run("receiving, offer keeps the screen", func(t *testing.T) {
		sess := newSharing(t, "recvonly")
		replacement := newGatewayH264Answerer(t)
		t.Cleanup(func() { replacement.Close() })
		sess.prepareAuxForReplacement(replacement, withScreen)
		if outcome := sess.TakeAuxResumeOutcome(); outcome.Lost || outcome.Sending {
			t.Fatalf("outcome = %+v, want kept and receiving", outcome)
		}
		found := false
		for _, transceiver := range replacement.GetTransceivers() {
			if sender := transceiver.Sender(); sender != nil && sender.Track() != nil && sender.Track().ID() == "screen" {
				found = true
			}
		}
		if !found {
			t.Fatal("the replacement PeerConnection must carry the screen track before the offer is applied")
		}
	})
	t.Run("no stream", func(t *testing.T) {
		sess := &Session{ID: "aux-resume-none", State: StateActive}
		sess.SetAuxUplinkMid("2")
		sess.prepareAuxForReplacement(nil, withScreen)
		if sess.AuxUplinkMid() != "" {
			t.Fatal("without a stream no auxiliary section is expected on the new PeerConnection")
		}
		if outcome := sess.TakeAuxResumeOutcome(); outcome.Lost {
			t.Fatalf("outcome = %+v, want nothing lost", outcome)
		}
	})
}

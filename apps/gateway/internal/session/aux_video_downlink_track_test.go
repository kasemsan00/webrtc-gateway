package session

import (
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/pion/rtp"
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

// Regression test for the mid-call PT mismatch bug (Brave/Chromium black screen):
// Initial call negotiates mid=0 on PT 102; stream_added renegotiation offers
// PT 127 + 102 on mid=0 and ONLY PT 127 (no 102) on mid=1. The gateway must
// keep mid=0 on PT 102, answer PT 127 on mid=1, and stamp outbound screen RTP
// packets on mid=1 with PT 127 (not MediaEngine's initial PT 102).
func TestAuxDownlinkPinsNegotiatedPayloadTypeOnMidCallRenegotiation(t *testing.T) {
	clientMedia := &webrtc.MediaEngine{}
	for _, codec := range []webrtc.RTPCodecParameters{
		h264CodecParameters(102, h264ConstrainedBaselineProfile, 1, nil),
		h264CodecParameters(127, h264BaselineProfile, 1, nil),
	} {
		if err := clientMedia.RegisterCodec(codec, webrtc.RTPCodecTypeVideo); err != nil {
			t.Fatal(err)
		}
	}
	client := newLoopbackPeerConnectionWithMediaEngine(t, clientMedia)
	defer client.Close()

	gateway := newGatewayH264Answerer(t)
	defer gateway.Close()
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

	// Initial call: mid=0 offers PT 102 only.
	camTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000}, "camera", "client-cam")
	if err != nil {
		t.Fatal(err)
	}
	camTx, err := client.AddTransceiverFromTrack(camTrack,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv})
	if err != nil {
		t.Fatal(err)
	}
	if err := camTx.SetCodecPreferences([]webrtc.RTPCodecParameters{
		h264CodecParameters(102, h264ConstrainedBaselineProfile, 1, nil),
	}); err != nil {
		t.Fatal(err)
	}
	offer1, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	g := webrtc.GatheringCompletePromise(client)
	if err := client.SetLocalDescription(offer1); err != nil {
		t.Fatal(err)
	}
	<-g
	if err := gateway.SetRemoteDescription(*client.LocalDescription()); err != nil {
		t.Fatal(err)
	}
	if err := PreferWebRTCH264PacketizationMode(gateway, client.LocalDescription().SDP, 1); err != nil {
		t.Fatal(err)
	}
	answer1, err := gateway.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	g = webrtc.GatheringCompletePromise(gateway)
	if err := gateway.SetLocalDescription(answer1); err != nil {
		t.Fatal(err)
	}
	<-g
	if err := client.SetRemoteDescription(*gateway.LocalDescription()); err != nil {
		t.Fatal(err)
	}

	// Mid-call stream_added: camera offers 127 then 102; screen recvonly offers 127 ONLY.
	sess := &Session{ID: "aux-pt-pin", RTPBufferSize: 2048, State: StateActive, PeerConnection: gateway}
	sess.SetSIPVideoPacketizationMode(1)
	rtpConn := listenLoopbackUDP(t)
	aux, err := sess.OpenAuxVideo(rtpConn, listenLoopbackUDP(t), udpAddrOf(rtpConn).Port, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.CloseAuxVideo("test-cleanup")
	aux.SetSIPDirection("recvonly")
	aux.SetRemoteEndpoints(udpAddrOf(listenLoopbackUDP(t)), nil, true)
	if err := sess.PrepareAuxDownlinkTrack(); err != nil {
		t.Fatal(err)
	}

	if err := camTx.SetCodecPreferences([]webrtc.RTPCodecParameters{
		h264CodecParameters(127, h264BaselineProfile, 1, nil),
		h264CodecParameters(102, h264ConstrainedBaselineProfile, 1, nil),
	}); err != nil {
		t.Fatal(err)
	}
	screenTx, err := client.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	if err != nil {
		t.Fatal(err)
	}
	if err := screenTx.SetCodecPreferences([]webrtc.RTPCodecParameters{
		h264CodecParameters(127, h264BaselineProfile, 1, nil),
	}); err != nil {
		t.Fatal(err)
	}

	screenTracks := make(chan *webrtc.TrackRemote, 1)
	client.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if track.StreamID() == "pion-screen-"+sess.ID || track.ID() == "screen" {
			screenTracks <- track
		}
	})

	offer2, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetLocalDescription(offer2); err != nil {
		t.Fatal(err)
	}
	answer2SDP, err := sess.AnswerClientOfferInPlace(client.LocalDescription().SDP)
	if err != nil {
		t.Fatalf("AnswerClientOfferInPlace: %v", err)
	}
	sections := parseOfferedVideoSections(answer2SDP)
	if len(sections) != 2 {
		t.Fatalf("expected 2 video sections in answer, got %d:\n%s", len(sections), answer2SDP)
	}
	if len(sections[0].payloads) == 0 || sections[0].payloads[0] != 102 {
		t.Fatalf("camera video section should stay on PT 102, got %v:\n%s", sections[0].payloads, answer2SDP)
	}
	if len(sections[1].payloads) == 0 || sections[1].payloads[0] != 127 {
		t.Fatalf("screen video section must answer PT 127, got %v:\n%s", sections[1].payloads, answer2SDP)
	}
	if err := client.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer2SDP}); err != nil {
		t.Fatalf("client SetRemoteDescription: %v", err)
	}
	if err := sess.AttachAuxDownlinkTrack(); err != nil {
		t.Fatalf("AttachAuxDownlinkTrack: %v", err)
	}

	// Send SIP RTP packets and verify the remote peer receives them with PT 127 on the wire.
	stopFeed := make(chan struct{})
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for seq := uint16(100); ; seq++ {
			select {
			case <-stopFeed:
				return
			case <-ticker.C:
				aux.HandleSIPRTP(auxDownlinkPacket(seq, auxTestIDR))
			}
		}
	}()
	defer close(stopFeed)

	select {
	case remoteTrack := <-screenTracks:
		if remoteTrack.PayloadType() != 127 {
			t.Fatalf("remote screen track payload type = %d, want 127", remoteTrack.PayloadType())
		}
		pkt, _, err := remoteTrack.ReadRTP()
		if err != nil {
			t.Fatalf("ReadRTP on screen track: %v", err)
		}
		if pkt.PayloadType != 127 {
			t.Fatalf("wire RTP PayloadType = %d, want 127", pkt.PayloadType)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for screen track with PT 127")
	}
}

// Regression test for re-share SRTP continuity: when Share #1 ends at high seq
// (50000) and Share #2 arrives on the reused WebRTC transceiver with a brand
// new SIP SSRC and backward seq (1000) + backward timestamp, egress sequence
// numbers must continue monotonically (50001, 50002, ...) and timestamps must
// advance forward so SRTP replay protection never drops Share #2.
func TestAuxDownlinkNormalizesSeqAndTimestampAcrossShares(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	track := newAuxDownlinkLocalTrack("screen", "pion-screen-test")
	track.nowFn = func() time.Time { return now }

	makePkt := func(ssrc uint32, seq uint16, ts uint32) []byte {
		p := rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    99,
				SequenceNumber: seq,
				Timestamp:      ts,
				SSRC:           ssrc,
			},
			Payload: auxTestIDR,
		}
		raw, err := p.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	// Share #1: SSRC 0x1111, seq 50000..50001, ts 900000..903000.
	out1, seq1, off1 := track.NormalizeDownlink(makePkt(0x1111, 50000, 900000))
	if seq1 != 50000 || off1 != 0 || binary.BigEndian.Uint32(out1[4:8]) != 900000 {
		t.Fatalf("share1 pkt1: seq=%d off=%d ts=%d", seq1, off1, binary.BigEndian.Uint32(out1[4:8]))
	}
	now = now.Add(33 * time.Millisecond)
	out2, seq2, _ := track.NormalizeDownlink(makePkt(0x1111, 50001, 903000))
	if seq2 != 50001 || binary.BigEndian.Uint32(out2[4:8]) != 903000 {
		t.Fatalf("share1 pkt2: seq=%d ts=%d", seq2, binary.BigEndian.Uint32(out2[4:8]))
	}

	// Share #2 after 2s gap: brand new SSRC 0x2222, backward seq 1000, backward ts 10000.
	now = now.Add(2 * time.Second)
	out3, seq3, off3 := track.NormalizeDownlink(makePkt(0x2222, 1000, 10000))
	ts3 := binary.BigEndian.Uint32(out3[4:8])
	if seq3 != 50002 {
		t.Fatalf("share2 pkt1 seq = %d, want 50002 (monotonic across SSRC change)", seq3)
	}
	if uint16(seq3-off3) != 1000 {
		t.Fatalf("share2 seq offset reverse map = %d, want 1000", uint16(seq3-off3))
	}
	if ts3 <= 903000 {
		t.Fatalf("share2 pkt1 ts = %d, want > 903000", ts3)
	}

	// Second slice of the same Share #2 frame keeps identical timestamp and increments seq.
	out4, seq4, _ := track.NormalizeDownlink(makePkt(0x2222, 1001, 10000))
	ts4 := binary.BigEndian.Uint32(out4[4:8])
	if seq4 != 50003 || ts4 != ts3 {
		t.Fatalf("share2 pkt2: seq=%d ts=%d, want seq=50003 ts=%d", seq4, ts4, ts3)
	}
}

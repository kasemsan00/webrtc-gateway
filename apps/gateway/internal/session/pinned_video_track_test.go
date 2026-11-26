package session

import (
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
)

// Regression test for frozen remote camera video during/after dual screen share:
// Initial call negotiates primary camera (mid=0/1) on PT 102. On a later in-place
// renegotiation (e.g. adding a 3rd video section for dual screen share), Chromium
// can retire PT 102 from the primary video section and offer a new H.264 PT (124).
// PreferWebRTCH264PacketizationMode answers PT 124 and MUST update the live primary
// VideoTrack binding so subsequent camera RTP packets are stamped with PT 124
// rather than the retired PT 102 (which Chromium drops, permanently freezing remote
// camera video even after screen sharing stops).
func TestPrimaryVideoTrackUpdatesPayloadTypeOnMidCallRenegotiation(t *testing.T) {
	clientMedia := &webrtc.MediaEngine{}
	for _, codec := range []webrtc.RTPCodecParameters{
		h264CodecParameters(102, h264ConstrainedBaselineProfile, 1, nil),
		h264CodecParameters(124, h264ConstrainedBaselineProfile, 1, nil),
	} {
		if err := clientMedia.RegisterCodec(codec, webrtc.RTPCodecTypeVideo); err != nil {
			t.Fatal(err)
		}
	}
	client := newLoopbackPeerConnectionWithMediaEngine(t, clientMedia)
	defer client.Close()

	cfg := &config.Config{}
	sess, err := NewSession("primary-pt-reneg", cfg, config.TURNConfig{})
	if err != nil {
		t.Fatal(err)
	}
	sess.SetSIPVideoPacketizationMode(1)
	defer func() {
		if pc := sess.DetachPeerConnection(); pc != nil {
			_ = pc.Close()
		}
	}()
	gateway := sess.PeerConnection
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

	remoteCamTracks := make(chan *webrtc.TrackRemote, 4)
	client.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if track.Kind() == webrtc.RTPCodecTypeVideo {
			remoteCamTracks <- track
		}
	})

	// Initial call: client offers PT 102 only on camera video.
	camOut, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000}, "camera", "client-cam")
	if err != nil {
		t.Fatal(err)
	}
	camTx, err := client.AddTransceiverFromTrack(camOut,
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

	// Pump primary camera RTP continuously from gateway -> client.
	stopFeed := make(chan struct{})
	defer close(stopFeed)
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for seq := uint16(1); ; seq++ {
			select {
			case <-stopFeed:
				return
			case <-ticker.C:
				pkt := &rtp.Packet{
					Header: rtp.Header{
						Version:        2,
						PayloadType:    96,
						SequenceNumber: seq,
						Timestamp:      uint32(seq) * 3000,
						SSRC:           0x12345678,
						Marker:         true,
					},
					Payload: auxTestIDR,
				}
				raw, err := pkt.Marshal()
				if err == nil {
					_, _ = sess.WriteVideoToWebRTC(raw)
				}
			}
		}
	}()

	// Verify initial camera track arrives with PT 102.
	var track102 *webrtc.TrackRemote
	select {
	case track102 = <-remoteCamTracks:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for initial remote camera track (PT 102)")
	}
	if track102.PayloadType() != 102 {
		t.Fatalf("initial remote camera PT = %d, want 102", track102.PayloadType())
	}

	// Mid-call renegotiation: client drops PT 102 and moves camera to PT 124.
	if err := camTx.SetCodecPreferences([]webrtc.RTPCodecParameters{
		h264CodecParameters(124, h264ConstrainedBaselineProfile, 1, nil),
	}); err != nil {
		t.Fatal(err)
	}
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
	if len(sections) == 0 || len(sections[0].payloads) == 0 || sections[0].payloads[0] != 124 {
		t.Fatalf("expected answer2 primary video PT 124, got %+v\n%s", sections, answer2SDP)
	}
	if err := client.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer2SDP}); err != nil {
		t.Fatalf("client SetRemoteDescription(answer2): %v", err)
	}

	// Verify the live primary VideoTrack binding immediately switched to PT 124
	// and packets arrive on the wire with PT 124.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_ = track102.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		pkt, _, err := track102.ReadRTP()
		if err == nil && pkt.PayloadType == 124 {
			return
		}
		select {
		case track124 := <-remoteCamTracks:
			if track124.PayloadType() == 124 {
				_ = track124.SetReadDeadline(time.Now().Add(2 * time.Second))
				pkt, _, err := track124.ReadRTP()
				if err != nil {
					t.Fatalf("read PT 124 camera packet: %v", err)
				}
				if pkt.PayloadType != 124 {
					t.Fatalf("wire camera PT = %d, want 124", pkt.PayloadType)
				}
				return
			}
		default:
		}
	}
	t.Fatal("primary camera RTP packets never switched to PT 124 after mid-call renegotiation")
}

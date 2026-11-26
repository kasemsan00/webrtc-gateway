package session

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

func TestAuxVideoMidFromOffer(t *testing.T) {
	audio := "m=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=mid:0\r\n"
	camera := "m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=mid:1\r\na=sendrecv\r\n"
	screen := "m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=mid:screen-2\r\na=sendonly\r\n"
	remoteScreen := "m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=mid:remote-screen\r\na=recvonly\r\n"
	for _, tc := range []struct {
		name    string
		sdp     string
		wantMid string
		wantOK  bool
	}{
		{name: "camera and screen", sdp: "v=0\r\n" + audio + camera + screen, wantMid: "screen-2", wantOK: true},
		{name: "camera, remote screen recvonly, and local screen sendonly", sdp: "v=0\r\n" + audio + camera + remoteScreen + screen, wantMid: "screen-2", wantOK: true},
		{name: "camera only", sdp: "v=0\r\n" + audio + camera},
		{name: "audio only", sdp: "v=0\r\n" + audio},
		{name: "three videos (ambiguous two sendonly)", sdp: "v=0\r\n" + audio + camera + screen + screen},
		{name: "rejected second video", sdp: "v=0\r\n" + audio + camera + strings.Replace(screen, "m=video 9 ", "m=video 0 ", 1)},
		{name: "second video without mid", sdp: "v=0\r\n" + audio + camera + "m=video 9 UDP/TLS/RTP/SAVPF 96\r\na=sendonly\r\n"},
		{name: "rejected camera does not count", sdp: "v=0\r\n" + audio + strings.Replace(camera, "m=video 9 ", "m=video 0 ", 1) + screen},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mid, ok := AuxVideoMidFromOffer(tc.sdp)
			if ok != tc.wantOK || mid != tc.wantMid {
				t.Fatalf("AuxVideoMidFromOffer = %q, %v; want %q, %v", mid, ok, tc.wantMid, tc.wantOK)
			}
		})
	}
}

func TestAnswerClientOfferInPlaceRequiresStablePeerConnection(t *testing.T) {
	sess := &Session{ID: "in-place-none", State: StateActive}
	if _, err := sess.AnswerClientOfferInPlace("v=0\r\n"); err == nil {
		t.Fatal("expected an error without a PeerConnection")
	}
	if _, err := sess.AnswerClientOfferInPlace("  "); err == nil {
		t.Fatal("expected an error for an empty offer")
	}
}

// The share flow on the WebRTC side: a call with camera video is running,
// the client adds a screen track and offers again, and the gateway answers on
// the same PeerConnection. The camera keeps its track and the screen reaches
// the auxiliary SIP endpoint.
func TestAnswerClientOfferInPlaceAddsScreenWithoutReplacingPeerConnection(t *testing.T) {
	gateway := newGatewayH264Answerer(t)
	t.Cleanup(func() { gateway.Close() })
	clientMedia, err := createCustomMediaEngine()
	if err != nil {
		t.Fatal(err)
	}
	client := newLoopbackPeerConnectionWithMediaEngine(t, clientMedia)
	t.Cleanup(func() { client.Close() })

	sess := &Session{ID: "in-place", RTPBufferSize: 2048, State: StateActive, PeerConnection: gateway}
	sess.SetSIPVideoPacketizationMode(1)
	routed := make(chan string, 4)
	gateway.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		mid := transceiverMidForReceiver(gateway, receiver)
		if sess.routeAuxUplinkTrack(gateway, track, receiver) {
			routed <- "aux:" + mid
			return
		}
		routed <- "primary:" + mid
		go func() {
			buf := make([]byte, 2048)
			for {
				if _, _, err := track.Read(buf); err != nil {
					return
				}
			}
		}()
	})

	addTrack := func(id string) *webrtc.TrackLocalStaticRTP {
		track, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000}, id, "client-"+id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.AddTrack(track); err != nil {
			t.Fatal(err)
		}
		return track
	}

	// Call setup: camera only. This first exchange gathers ICE candidates.
	camera := addTrack("camera")
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

	// The share: SIP side is open, then the client's new offer is applied.
	rtpConn := listenLoopbackUDP(t)
	aux, err := sess.OpenAuxVideo(rtpConn, listenLoopbackUDP(t), udpAddrOf(rtpConn).Port, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.CloseAuxVideo("test-cleanup") })
	sipPeer := listenLoopbackUDP(t)
	aux.SetRemoteEndpoints(udpAddrOf(sipPeer), nil, true)

	screen := addTrack("screen")
	shareOffer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetLocalDescription(shareOffer); err != nil {
		t.Fatal(err)
	}
	screenMid, ok := AuxVideoMidFromOffer(shareOffer.SDP)
	if !ok {
		t.Fatalf("client offer has no second video section:\n%s", shareOffer.SDP)
	}
	sess.SetAuxUplinkMid(screenMid)

	shareAnswer, err := sess.AnswerClientOfferInPlace(shareOffer.SDP)
	if err != nil {
		t.Fatalf("in-place renegotiation failed: %v", err)
	}
	if sess.PeerConnection != gateway {
		t.Fatal("the PeerConnection must not be replaced")
	}
	if got := strings.Count(shareAnswer, "m=video "); got != 2 {
		t.Fatalf("answer has %d video sections, want 2:\n%s", got, shareAnswer)
	}
	if gateway.SignalingState() != webrtc.SignalingStateStable {
		t.Fatalf("gateway signaling state = %s, want stable", gateway.SignalingState())
	}
	if err := client.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: shareAnswer}); err != nil {
		t.Fatalf("client rejected the gateway answer: %v", err)
	}

	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		var seq uint16
		var timestamp uint32
		write := func(track *webrtc.TrackLocalStaticRTP, marker bool, payload []byte) {
			seq++
			_ = track.WriteRTP(&rtp.Packet{
				Header:  rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: timestamp, Marker: marker},
				Payload: payload,
			})
		}
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				timestamp += 3000
				write(camera, true, auxTestP)
				write(screen, false, auxTestSPS)
				write(screen, false, auxTestPPS)
				write(screen, true, auxTestIDR)
			}
		}
	}()

	seen := map[string]bool{}
	deadline := time.After(15 * time.Second)
	for len(seen) < 2 {
		select {
		case route := <-routed:
			seen[route] = true
		case <-deadline:
			t.Fatalf("tracks not routed in time, saw %v", seen)
		}
	}
	if !seen["aux:"+screenMid] {
		t.Fatalf("routes = %v, want the screen on the auxiliary pipeline (mid %s)", seen, screenMid)
	}

	var got []rtp.Packet
	buf := make([]byte, 2048)
	for len(got) < 3 {
		_ = sipPeer.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, _, err := sipPeer.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("auxiliary SIP endpoint received %d packets, want one whole picture: %v", len(got), err)
		}
		var packet rtp.Packet
		if err := packet.Unmarshal(append([]byte(nil), buf[:n]...)); err != nil {
			t.Fatal(err)
		}
		got = append(got, packet)
	}
	assertPayloads(t, got, auxTestSPS, auxTestPPS, auxTestIDR)
}

// Re-sharing in the same call keeps the WebRTC transceiver (no new offer/OnTrack):
// closing the first AuxVideo and opening a second one on the same Session must
// preserve the bound uplink track so the gateway sends browser PLIs and forwards
// the new share's keyframes without renegotiation.
func TestAuxUplinkRebindsAndForwardsOnSecondShareWithoutRenegotiation(t *testing.T) {
	gateway := newGatewayH264Answerer(t)
	t.Cleanup(func() { gateway.Close() })
	clientMedia, err := createCustomMediaEngine()
	if err != nil {
		t.Fatal(err)
	}
	client := newLoopbackPeerConnectionWithMediaEngine(t, clientMedia)
	t.Cleanup(func() { client.Close() })

	sess := &Session{ID: "reshare-in-place", RTPBufferSize: 2048, State: StateActive, PeerConnection: gateway}
	sess.SetSIPVideoPacketizationMode(1)
	routed := make(chan string, 4)
	gateway.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		mid := transceiverMidForReceiver(gateway, receiver)
		if sess.routeAuxUplinkTrack(gateway, track, receiver) {
			routed <- "aux:" + mid
			return
		}
		routed <- "primary:" + mid
		go func() {
			buf := make([]byte, 2048)
			for {
				if _, _, err := track.Read(buf); err != nil {
					return
				}
			}
		}()
	})

	addTrack := func(id string) *webrtc.TrackLocalStaticRTP {
		track, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000}, id, "client-"+id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.AddTrack(track); err != nil {
			t.Fatal(err)
		}
		return track
	}

	camera := addTrack("camera")
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

	// Share #1: adds second video m-section -> OnTrack fires -> forwards to sipPeer1.
	rtpConn1 := listenLoopbackUDP(t)
	if _, err := sess.OpenAuxVideo(rtpConn1, listenLoopbackUDP(t), udpAddrOf(rtpConn1).Port, 0); err != nil {
		t.Fatal(err)
	}
	sipPeer1 := listenLoopbackUDP(t)
	sess.AuxVideo().SetRemoteEndpoints(udpAddrOf(sipPeer1), nil, true)

	screen := addTrack("screen")
	shareOffer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetLocalDescription(shareOffer); err != nil {
		t.Fatal(err)
	}
	screenMid, ok := AuxVideoMidFromOffer(shareOffer.SDP)
	if !ok {
		t.Fatalf("client offer has no second video section:\n%s", shareOffer.SDP)
	}
	sess.SetAuxUplinkMid(screenMid)
	shareAnswer, err := sess.AnswerClientOfferInPlace(shareOffer.SDP)
	if err != nil {
		t.Fatalf("in-place renegotiation failed: %v", err)
	}
	if err := client.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: shareAnswer}); err != nil {
		t.Fatalf("client rejected share answer: %v", err)
	}

	// Drain client RTCP on screen sender so browser PLIs can be observed if needed.
	for _, sender := range client.GetSenders() {
		if sender.Track() == screen {
			go func(s *webrtc.RTPSender) {
				b := make([]byte, 1500)
				for {
					if _, _, err := s.Read(b); err != nil {
						return
					}
				}
			}(sender)
		}
	}

	var seq uint16
	var timestamp uint32
	write := func(track *webrtc.TrackLocalStaticRTP, marker bool, payload []byte) {
		seq++
		_ = track.WriteRTP(&rtp.Packet{
			Header:  rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: timestamp, Marker: marker},
			Payload: payload,
		})
	}
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				timestamp += 3000
				write(camera, true, auxTestP)
				write(screen, false, auxTestSPS)
				write(screen, false, auxTestPPS)
				write(screen, true, auxTestIDR)
			}
		}
	}()

	seen := map[string]bool{}
	deadline := time.After(15 * time.Second)
	for len(seen) < 2 {
		select {
		case route := <-routed:
			seen[route] = true
		case <-deadline:
			t.Fatalf("tracks not routed in time, saw %v", seen)
		}
	}
	if !seen["aux:"+screenMid] {
		t.Fatalf("routes = %v, want screen on aux mid %s", seen, screenMid)
	}

	readThree := func(conn *net.UDPConn) {
		var got []rtp.Packet
		buf := make([]byte, 2048)
		for len(got) < 3 {
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				t.Fatalf("received %d packets, want 3: %v", len(got), err)
			}
			var packet rtp.Packet
			if err := packet.Unmarshal(append([]byte(nil), buf[:n]...)); err != nil {
				t.Fatal(err)
			}
			got = append(got, packet)
		}
		assertPayloads(t, got, auxTestSPS, auxTestPPS, auxTestIDR)
	}
	readThree(sipPeer1)

	// Stop Share #1.
	if !sess.CloseAuxVideo("client-remove") {
		t.Fatal("expected Share #1 aux video to close")
	}

	// Share #2: open new AuxVideo WITHOUT WebRTC renegotiation / OnTrack.
	rtpConn2 := listenLoopbackUDP(t)
	aux2, err := sess.OpenAuxVideo(rtpConn2, listenLoopbackUDP(t), udpAddrOf(rtpConn2).Port, 0)
	if err != nil {
		t.Fatalf("OpenAuxVideo for Share #2: %v", err)
	}
	t.Cleanup(func() { sess.CloseAuxVideo("test-cleanup") })
	sipPeer2 := listenLoopbackUDP(t)
	aux2.SetRemoteEndpoints(udpAddrOf(sipPeer2), nil, true)

	if stats := aux2.Stats(); stats.BrowserPLISent < 1 {
		t.Fatalf("Share #2 BrowserPLISent = %d, want >= 1 from pre-bound uplink track", stats.BrowserPLISent)
	}
	readThree(sipPeer2)
}

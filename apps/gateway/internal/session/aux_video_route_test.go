package session

import (
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// A client that sends camera and screen as two video m-lines on one
// PeerConnection. The gateway must send only the track on the auxiliary mid
// to the auxiliary pipeline and leave the camera track on the primary path.
func TestOnTrackRoutesAuxiliaryMidToAuxPipeline(t *testing.T) {
	newPC := func() *webrtc.PeerConnection {
		mediaEngine, err := createCustomMediaEngine()
		if err != nil {
			t.Fatal(err)
		}
		pc := newLoopbackPeerConnectionWithMediaEngine(t, mediaEngine)
		t.Cleanup(func() { pc.Close() })
		return pc
	}
	gateway := newPC()
	client := newPC()

	newTrack := func(id string) *webrtc.TrackLocalStaticRTP {
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
	camera := newTrack("camera")
	screen := newTrack("screen")

	offer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	offerGathered := webrtc.GatheringCompletePromise(client)
	if err := client.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-offerGathered
	transceivers := client.GetTransceivers()
	if len(transceivers) != 2 {
		t.Fatalf("client transceivers = %d, want 2", len(transceivers))
	}
	cameraMid, screenMid := transceivers[0].Mid(), transceivers[1].Mid()
	if cameraMid == "" || screenMid == "" || cameraMid == screenMid {
		t.Fatalf("unexpected mids camera=%q screen=%q", cameraMid, screenMid)
	}

	sess := &Session{ID: "aux-route", RTPBufferSize: 2048, State: StateActive}
	rtpConn := listenLoopbackUDP(t)
	aux, err := sess.OpenAuxVideo(rtpConn, listenLoopbackUDP(t), udpAddrOf(rtpConn).Port, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.CloseAuxVideo("test-cleanup") })
	sipPeer := listenLoopbackUDP(t)
	aux.SetRemoteEndpoints(udpAddrOf(sipPeer), nil, true)
	// The mid is set before the offer is applied, as the signaling flow does.
	sess.SetAuxUplinkMid(screenMid)

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

	if err := gateway.SetRemoteDescription(*client.LocalDescription()); err != nil {
		t.Fatal(err)
	}
	answer, err := gateway.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	answerGathered := webrtc.GatheringCompletePromise(gateway)
	if err := gateway.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	<-answerGathered
	if err := client.SetRemoteDescription(*gateway.LocalDescription()); err != nil {
		t.Fatal(err)
	}

	// Keep sending whole pictures on both tracks until both have been seen;
	// packets written before DTLS is up are lost.
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
	if !seen["aux:"+screenMid] || !seen["primary:"+cameraMid] {
		t.Fatalf("routes = %v, want aux:%s and primary:%s", seen, screenMid, cameraMid)
	}

	// The screen track's pictures must arrive at the auxiliary SIP endpoint.
	// The sender never pauses, so read a fixed number of packets.
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
	if stats := aux.Stats(); stats.UplinkKeyframes == 0 {
		t.Fatalf("stats = %+v, want a forwarded keyframe", stats)
	}
}

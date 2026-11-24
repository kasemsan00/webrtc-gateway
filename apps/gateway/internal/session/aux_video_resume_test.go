package session

import (
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"webrtc-sip-gateway/internal/config"
)

// A resume replaces the PeerConnection. These tests run the real resume path
// (RenegotiatePeerConnection) against a client PeerConnection and check that
// an open screen stream continues on the replacement.

func newResumableSession(t *testing.T) *Session {
	t.Helper()
	mgr := NewManager(&config.Config{RTP: config.RTPConfig{BufferSize: 2048}})
	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() { mgr.DeleteSession(sess.ID) })
	sess.SetState(StateActive)
	sess.SetSIPVideoPacketizationMode(1)
	return sess
}

func newResumeClient(t *testing.T) *webrtc.PeerConnection {
	t.Helper()
	media, err := createCustomMediaEngine()
	if err != nil {
		t.Fatal(err)
	}
	client := newLoopbackPeerConnectionWithMediaEngine(t, media)
	t.Cleanup(func() { client.Close() })
	return client
}

func clientTrack(t *testing.T, client *webrtc.PeerConnection, id string) *webrtc.TrackLocalStaticRTP {
	t.Helper()
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

// resume sends the client's offer through the gateway's resume path and
// applies the answer on the client.
func resume(t *testing.T, sess *Session, client *webrtc.PeerConnection) string {
	t.Helper()
	offer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(client)
	if err := client.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	offerSDP := client.LocalDescription().SDP
	if err := sess.RenegotiatePeerConnection(offerSDP, config.TURNConfig{}, false); err != nil {
		t.Fatalf("resume renegotiation failed: %v", err)
	}
	sess.mu.RLock()
	pc := sess.PeerConnection
	sess.mu.RUnlock()
	if pc == nil || pc.LocalDescription() == nil {
		t.Fatal("no answer after resume")
	}
	if err := client.SetRemoteDescription(*pc.LocalDescription()); err != nil {
		t.Fatalf("client rejected the resume answer: %v", err)
	}
	return offerSDP
}

func TestResumeKeepsSharedScreenOnSharingSession(t *testing.T) {
	sess := newResumableSession(t)
	rtpConn := listenLoopbackUDP(t)
	aux, err := sess.OpenAuxVideo(rtpConn, listenLoopbackUDP(t), udpAddrOf(rtpConn).Port, 0)
	if err != nil {
		t.Fatal(err)
	}
	sipPeer := listenLoopbackUDP(t)
	aux.SetRemoteEndpoints(udpAddrOf(sipPeer), nil, true)
	sess.SetAuxUplinkMid("stale-mid-of-the-old-peer-connection")

	client := newResumeClient(t)
	clientTrack(t, client, "camera")
	screen := clientTrack(t, client, "screen")

	offerSDP := resume(t, sess, client)

	wantMid, ok := AuxVideoMidFromOffer(offerSDP)
	if !ok {
		t.Fatalf("resume offer has no second video section:\n%s", offerSDP)
	}
	if got := sess.AuxUplinkMid(); got != wantMid {
		t.Fatalf("auxiliary mid = %q, want the resume offer's %q", got, wantMid)
	}
	if outcome := sess.TakeAuxResumeOutcome(); outcome.Lost || !outcome.Sending {
		t.Fatalf("resume outcome = %+v, want the stream kept on a sending session", outcome)
	}
	if sess.AuxVideo() != aux {
		t.Fatal("the SIP side of the stream must stay open across the resume")
	}

	// The screen track on the replacement PeerConnection reaches the same
	// auxiliary SIP endpoint.
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		var seq uint16
		var timestamp uint32
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				timestamp += 3000
				for i, payload := range [][]byte{auxTestSPS, auxTestPPS, auxTestIDR} {
					seq++
					_ = screen.WriteRTP(&rtp.Packet{
						Header:  rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: timestamp, Marker: i == 2},
						Payload: payload,
					})
				}
			}
		}
	}()
	var got []rtp.Packet
	buf := make([]byte, 2048)
	for len(got) < 3 {
		_ = sipPeer.SetReadDeadline(time.Now().Add(15 * time.Second))
		n, _, err := sipPeer.ReadFromUDP(buf)
		if err != nil {
			t.Fatalf("auxiliary SIP endpoint received %d packets after resume: %v (stats=%+v)", len(got), err, aux.Stats())
		}
		var packet rtp.Packet
		if err := packet.Unmarshal(append([]byte(nil), buf[:n]...)); err != nil {
			t.Fatal(err)
		}
		got = append(got, packet)
	}
	assertPayloads(t, got, auxTestSPS, auxTestPPS, auxTestIDR)
}

func TestResumeKeepsReceivedScreenOnReceivingSession(t *testing.T) {
	sess := newResumableSession(t)
	rtpConn := listenLoopbackUDP(t)
	aux, err := sess.OpenAuxVideo(rtpConn, listenLoopbackUDP(t), udpAddrOf(rtpConn).Port, 0)
	if err != nil {
		t.Fatal(err)
	}
	aux.SetSIPDirection("recvonly")
	aux.SetRemoteEndpoints(udpAddrOf(listenLoopbackUDP(t)), nil, true)

	client := newResumeClient(t)
	clientTrack(t, client, "camera")
	if _, err := client.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	screenTracks := make(chan *webrtc.TrackRemote, 1)
	client.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if track.ID() == "screen" {
			screenTracks <- track
		}
	})

	resume(t, sess, client)

	if outcome := sess.TakeAuxResumeOutcome(); outcome.Lost || outcome.Sending {
		t.Fatalf("resume outcome = %+v, want the stream kept on a receiving session", outcome)
	}
	if !sess.HasNegotiatedAuxDownlink() {
		t.Fatal("the replacement PeerConnection must carry the negotiated screen section")
	}

	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		seq := uint16(500)
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
	case <-screenTracks:
	case <-time.After(15 * time.Second):
		t.Fatalf("the client did not receive the screen track after resume; stats=%+v", aux.Stats())
	}
}

func TestResumeWithoutScreenSectionReportsTheStreamLost(t *testing.T) {
	sess := newResumableSession(t)
	rtpConn := listenLoopbackUDP(t)
	if _, err := sess.OpenAuxVideo(rtpConn, listenLoopbackUDP(t), udpAddrOf(rtpConn).Port, 0); err != nil {
		t.Fatal(err)
	}
	sess.SetAuxUplinkMid("2")

	client := newResumeClient(t)
	clientTrack(t, client, "camera")

	resume(t, sess, client)

	outcome := sess.TakeAuxResumeOutcome()
	if !outcome.Lost || !outcome.Sending {
		t.Fatalf("resume outcome = %+v, want the stream lost on a sending session", outcome)
	}
	if sess.AuxUplinkMid() != "" {
		t.Fatal("the auxiliary mid of the old PeerConnection must be cleared")
	}
}

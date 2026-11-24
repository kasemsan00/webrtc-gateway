package sip

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

func newAuxTestServer(t *testing.T, slots int) *Server {
	t.Helper()
	portMin, portMax := findFreeContiguousUDPBlock(t, slots)
	return &Server{rtpConfig: config.RTPConfig{PortMin: portMin, PortMax: portMax, BufferSize: 2048}}
}

func TestBindRTPPortPairTakesOneSlotAndQuadSkipsIt(t *testing.T) {
	srv := newAuxTestServer(t, 2)

	pairPort, pairRTP, pairRTCP, err := srv.bindRTPPortPair()
	if err != nil {
		t.Fatalf("bindRTPPortPair: %v", err)
	}
	defer pairRTP.Close()
	defer pairRTCP.Close()
	if (pairPort-srv.rtpConfig.PortMin)%rtpPortSlotStep != 0 {
		t.Fatalf("pair port %d is not on a slot boundary from %d", pairPort, srv.rtpConfig.PortMin)
	}
	if got := pairRTCP.LocalAddr().(*net.UDPAddr).Port; got != pairPort+1 {
		t.Fatalf("pair RTCP port = %d, want %d", got, pairPort+1)
	}

	quadPort, a, ar, v, vr, err := srv.bindRTPPortQuad()
	if err != nil {
		t.Fatalf("bindRTPPortQuad with one free slot: %v", err)
	}
	defer a.Close()
	defer ar.Close()
	defer v.Close()
	defer vr.Close()
	if quadPort == pairPort {
		t.Fatalf("quad reused the pair's slot at %d", quadPort)
	}

	if _, _, _, err := srv.bindRTPPortPair(); err == nil {
		t.Fatal("expected no slot left for a second pair")
	}
}

func TestOpenAuxVideoForSessionReadsBothSocketsAndStopsOnClose(t *testing.T) {
	srv := newAuxTestServer(t, 1)
	sess := &session.Session{ID: "aux-sip", RTPBufferSize: 2048, State: session.StateActive}

	aux, err := srv.openAuxVideoForSession(sess)
	if err != nil {
		t.Fatalf("openAuxVideoForSession: %v", err)
	}
	if _, err := srv.openAuxVideoForSession(sess); err == nil {
		t.Fatal("a second auxiliary stream must be refused")
	}
	rtpPort, rtcpPort := aux.Ports()
	if rtpPort != srv.rtpConfig.PortMin || rtcpPort != rtpPort+1 {
		t.Fatalf("ports = %d/%d, want %d/%d", rtpPort, rtcpPort, srv.rtpConfig.PortMin, srv.rtpConfig.PortMin+1)
	}

	var mu sync.Mutex
	browserRequests := 0
	aux.BindUplinkTrack(0xCAFE, func([]rtcp.Packet) error {
		mu.Lock()
		browserRequests++
		mu.Unlock()
		return nil
	})

	peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	send := func(port int, data []byte) {
		t.Helper()
		if _, err := peer.WriteToUDP(data, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}); err != nil {
			t.Fatalf("send to %d: %v", port, err)
		}
	}
	media, _ := (&rtp.Packet{
		Header:  rtp.Header{Version: 2, PayloadType: 99, SequenceNumber: 1, SSRC: 0x51515151},
		Payload: []byte{0x65, 0x01},
	}).Marshal()
	pli, _ := rtcp.Marshal([]rtcp.Packet{
		&rtcp.ReceiverReport{SSRC: 1},
		&rtcp.PictureLossIndication{SenderSSRC: 1, MediaSSRC: 2},
	})

	// RTP on the RTP port, RTP misdelivered to the RTCP port, and RTCP
	// multiplexed on the RTP port must all be classified by content.
	send(rtpPort, media)
	send(rtcpPort, media)
	send(rtpPort, pli)

	waitFor(t, "auxiliary packets to be read", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return aux.Stats().DownlinkPackets == 2 && browserRequests == 1
	})

	sess.CloseMediaTransports()

	if sess.AuxVideo() != nil {
		t.Fatal("media teardown must close the auxiliary stream")
	}
	waitFor(t, "auxiliary ports to be released", func() bool {
		_, rtpConn, rtcpConn, err := srv.bindRTPPortPair()
		if err != nil {
			return false
		}
		rtpConn.Close()
		rtcpConn.Close()
		return true
	})
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

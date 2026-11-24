package session

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

var (
	auxTestSPS = []byte{0x67, 0x42, 0xe0, 0x1f, 0x11}
	auxTestPPS = []byte{0x68, 0xce, 0x06, 0xe2}
	auxTestIDR = []byte{0x65, 0x88, 0x84, 0x00, 0x21}
	auxTestP   = []byte{0x41, 0x9a, 0x00, 0x10}
)

func listenLoopbackUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func udpAddrOf(conn *net.UDPConn) *net.UDPAddr {
	return conn.LocalAddr().(*net.UDPAddr)
}

// auxTestRig is a session with an open auxiliary stream whose SIP peer is a
// pair of loopback sockets the test reads from.
type auxTestRig struct {
	sess     *Session
	aux      *AuxVideo
	peerRTP  *net.UDPConn
	peerRTCP *net.UDPConn
}

func newAuxTestRig(t *testing.T) *auxTestRig {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sess := &Session{ID: "aux-test", RTPBufferSize: 2048, State: StateActive, ctx: ctx, cancel: cancel}
	rtpConn := listenLoopbackUDP(t)
	rtcpConn := listenLoopbackUDP(t)
	aux, err := sess.OpenAuxVideo(rtpConn, rtcpConn, udpAddrOf(rtpConn).Port, udpAddrOf(rtcpConn).Port)
	if err != nil {
		t.Fatalf("OpenAuxVideo: %v", err)
	}
	t.Cleanup(func() { sess.CloseAuxVideo("test-cleanup") })

	rig := &auxTestRig{sess: sess, aux: aux, peerRTP: listenLoopbackUDP(t), peerRTCP: listenLoopbackUDP(t)}
	aux.SetSIPFormat(99, 1)
	aux.SetRemoteEndpoints(udpAddrOf(rig.peerRTP), udpAddrOf(rig.peerRTCP), false)
	return rig
}

func browserPacket(seq uint16, timestamp uint32, marker bool, payload []byte) *rtp.Packet {
	return &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    102,
			SequenceNumber: seq,
			Timestamp:      timestamp,
			SSRC:           0xB0B0B0B0,
			Marker:         marker,
			Extension:      true,
			Extensions:     []rtp.Extension{{}},
			CSRC:           []uint32{7},
		},
		Payload: append([]byte(nil), payload...),
	}
}

// readRTP reads datagrams until none arrives for a short while.
func readRTP(t *testing.T, conn *net.UDPConn) []rtp.Packet {
	t.Helper()
	var packets []rtp.Packet
	buf := make([]byte, 65535)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return packets
		}
		var packet rtp.Packet
		if err := packet.Unmarshal(append([]byte(nil), buf[:n]...)); err != nil {
			t.Fatalf("unmarshal forwarded packet: %v", err)
		}
		packets = append(packets, packet)
	}
}

func readRTCP(t *testing.T, conn *net.UDPConn) []rtcp.Packet {
	t.Helper()
	buf := make([]byte, 1500)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil
	}
	packets, err := rtcp.Unmarshal(buf[:n])
	if err != nil {
		t.Fatalf("unmarshal RTCP: %v", err)
	}
	return packets
}

func payloadsOf(packets []rtp.Packet) [][]byte {
	out := make([][]byte, 0, len(packets))
	for _, packet := range packets {
		out = append(out, packet.Payload)
	}
	return out
}

func assertPayloads(t *testing.T, packets []rtp.Packet, want ...[]byte) {
	t.Helper()
	got := payloadsOf(packets)
	if len(got) != len(want) {
		t.Fatalf("forwarded %d packets, want %d: %x", len(got), len(want), got)
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("packet %d payload = %x, want %x", i, got[i], want[i])
		}
	}
}

func TestAuxUplinkHoldsUntilIDRAndInjectsParameterSets(t *testing.T) {
	rig := newAuxTestRig(t)

	rig.aux.HandleUplinkRTP(browserPacket(1, 1000, true, auxTestP))
	rig.aux.HandleUplinkRTP(browserPacket(2, 2000, false, auxTestSPS))
	rig.aux.HandleUplinkRTP(browserPacket(3, 2000, false, auxTestPPS))
	rig.aux.HandleUplinkRTP(browserPacket(4, 2000, true, auxTestIDR))
	rig.aux.HandleUplinkRTP(browserPacket(5, 5000, true, auxTestP))

	got := readRTP(t, rig.peerRTP)
	assertPayloads(t, got, auxTestSPS, auxTestPPS, auxTestIDR, auxTestP)

	ssrc := got[0].SSRC
	if ssrc == 0 || ssrc == 0xB0B0B0B0 {
		t.Fatalf("auxiliary SSRC = %d, want its own non-zero SSRC", ssrc)
	}
	for i, packet := range got {
		if packet.SSRC != ssrc || packet.PayloadType != 99 {
			t.Fatalf("packet %d ssrc=%d pt=%d, want ssrc=%d pt=99", i, packet.SSRC, packet.PayloadType, ssrc)
		}
		if packet.Extension || len(packet.CSRC) != 0 || packet.Padding {
			t.Fatalf("packet %d kept browser header extras: %+v", i, packet.Header)
		}
		if i > 0 && packet.SequenceNumber != got[i-1].SequenceNumber+1 {
			t.Fatalf("sequence not contiguous at %d: %d after %d", i, packet.SequenceNumber, got[i-1].SequenceNumber)
		}
	}
	if got[0].Timestamp != 2000 || got[1].Timestamp != 2000 || got[0].Marker || got[1].Marker {
		t.Fatalf("parameter sets must carry the IDR timestamp without a marker: %+v %+v", got[0].Header, got[1].Header)
	}
	if !got[2].Marker || !got[3].Marker {
		t.Fatal("picture markers must be preserved")
	}
	if stats := rig.aux.Stats(); stats.UplinkHeld != 3 || stats.UplinkKeyframes != 1 {
		t.Fatalf("stats = %+v, want 3 held and 1 keyframe", stats)
	}
}

func TestAuxUplinkInjectsOncePerAccessUnit(t *testing.T) {
	rig := newAuxTestRig(t)
	fuStart := []byte{0x7c, 0x85, 0x01, 0x02}
	fuMiddle := []byte{0x7c, 0x05, 0x03, 0x04}
	fuEnd := []byte{0x7c, 0x45, 0x05, 0x06}
	secondSlice := []byte{0x7c, 0x85, 0x07, 0x08}

	rig.aux.HandleUplinkRTP(browserPacket(1, 900, false, auxTestSPS))
	rig.aux.HandleUplinkRTP(browserPacket(2, 900, false, auxTestPPS))
	rig.aux.HandleUplinkRTP(browserPacket(3, 3000, false, fuStart))
	rig.aux.HandleUplinkRTP(browserPacket(4, 3000, false, fuMiddle))
	rig.aux.HandleUplinkRTP(browserPacket(5, 3000, false, fuEnd))
	rig.aux.HandleUplinkRTP(browserPacket(6, 3000, true, secondSlice))

	assertPayloads(t, readRTP(t, rig.peerRTP), auxTestSPS, auxTestPPS, fuStart, fuMiddle, fuEnd, secondSlice)
}

func TestAuxUplinkSkipsInjectionWhenBrowserSentParameterSetsInBand(t *testing.T) {
	rig := newAuxTestRig(t)
	rig.aux.HandleUplinkRTP(browserPacket(1, 1000, false, auxTestSPS))
	rig.aux.HandleUplinkRTP(browserPacket(2, 1000, false, auxTestPPS))
	rig.aux.HandleUplinkRTP(browserPacket(3, 1000, true, auxTestIDR))
	_ = readRTP(t, rig.peerRTP)

	// The gate is open now, so in-band parameter sets are forwarded as they
	// come and the IDR that follows must not get a second pair.
	newSPS := []byte{0x67, 0x42, 0xe0, 0x28, 0x22}
	rig.aux.HandleUplinkRTP(browserPacket(4, 4000, false, newSPS))
	rig.aux.HandleUplinkRTP(browserPacket(5, 4000, false, auxTestPPS))
	rig.aux.HandleUplinkRTP(browserPacket(6, 4000, true, auxTestIDR))

	assertPayloads(t, readRTP(t, rig.peerRTP), newSPS, auxTestPPS, auxTestIDR)
}

func TestAuxUplinkDeaggregatesSTAPA(t *testing.T) {
	rig := newAuxTestRig(t)
	stapA := []byte{0x78}
	for _, nal := range [][]byte{auxTestSPS, auxTestPPS, auxTestIDR} {
		stapA = append(stapA, byte(len(nal)>>8), byte(len(nal)))
		stapA = append(stapA, nal...)
	}

	rig.aux.HandleUplinkRTP(browserPacket(1, 7000, true, stapA))

	got := readRTP(t, rig.peerRTP)
	assertPayloads(t, got, auxTestSPS, auxTestPPS, auxTestIDR)
	if got[0].Marker || got[1].Marker || !got[2].Marker {
		t.Fatalf("only the last de-aggregated NAL carries the marker: %v %v %v", got[0].Marker, got[1].Marker, got[2].Marker)
	}
}

func TestAuxUplinkMode0ReassemblesFUA(t *testing.T) {
	rig := newAuxTestRig(t)
	rig.aux.SetSIPFormat(99, 0)
	rig.aux.HandleUplinkRTP(browserPacket(1, 100, false, auxTestSPS))
	rig.aux.HandleUplinkRTP(browserPacket(2, 100, false, auxTestPPS))
	rig.aux.HandleUplinkRTP(browserPacket(3, 3000, false, []byte{0x7c, 0x85, 0x01, 0x02}))
	rig.aux.HandleUplinkRTP(browserPacket(4, 3000, true, []byte{0x7c, 0x45, 0x03, 0x04}))

	assertPayloads(t, readRTP(t, rig.peerRTP), auxTestSPS, auxTestPPS, []byte{0x65, 0x01, 0x02, 0x03, 0x04})
}

func TestAuxUplinkWithoutSIPEndpointSendsNothingAndKeepsGateClosed(t *testing.T) {
	sess := &Session{ID: "aux-no-endpoint", RTPBufferSize: 2048, State: StateActive}
	rtpConn := listenLoopbackUDP(t)
	aux, err := sess.OpenAuxVideo(rtpConn, listenLoopbackUDP(t), udpAddrOf(rtpConn).Port, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.CloseAuxVideo("test-cleanup") })

	aux.HandleUplinkRTP(browserPacket(1, 1000, false, auxTestSPS))
	aux.HandleUplinkRTP(browserPacket(2, 1000, false, auxTestPPS))
	aux.HandleUplinkRTP(browserPacket(3, 1000, true, auxTestIDR))
	if stats := aux.Stats(); stats.UplinkForwarded != 0 || stats.UplinkHeld != 3 {
		t.Fatalf("stats = %+v, want nothing forwarded", stats)
	}

	// Once the endpoint is known the next IDR opens the stream, using the
	// parameter sets cached while it was unknown.
	peer := listenLoopbackUDP(t)
	aux.SetRemoteEndpoints(udpAddrOf(peer), nil, true)
	aux.HandleUplinkRTP(browserPacket(4, 2000, true, auxTestP))
	aux.HandleUplinkRTP(browserPacket(5, 3000, true, auxTestIDR))

	assertPayloads(t, readRTP(t, peer), auxTestSPS, auxTestPPS, auxTestIDR)
}

func TestAuxUplinkRequestsBrowserKeyframeWhileWaiting(t *testing.T) {
	rig := newAuxTestRig(t)
	var mu sync.Mutex
	var sent []rtcp.Packet
	rig.aux.BindUplinkTrack(0xCAFE, func(packets []rtcp.Packet) error {
		mu.Lock()
		sent = append(sent, packets...)
		mu.Unlock()
		return nil
	})

	rig.aux.HandleUplinkRTP(browserPacket(1, 1000, true, auxTestP))
	rig.aux.HandleUplinkRTP(browserPacket(2, 2000, true, auxTestP))

	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("browser keyframe requests = %d, want 1 (rate limited)", len(sent))
	}
	pli, ok := sent[0].(*rtcp.PictureLossIndication)
	if !ok || pli.MediaSSRC != 0xCAFE {
		t.Fatalf("request = %#v, want PLI for the auxiliary track SSRC", sent[0])
	}
}

func TestAuxSIPKeyframeRequestGoesToBrowserForAuxTrack(t *testing.T) {
	rig := newAuxTestRig(t)
	var got []rtcp.Packet
	rig.aux.BindUplinkTrack(0xCAFE, func(packets []rtcp.Packet) error {
		got = append(got, packets...)
		return nil
	})
	raw, err := rtcp.Marshal([]rtcp.Packet{
		&rtcp.ReceiverReport{SSRC: 1},
		&rtcp.PictureLossIndication{SenderSSRC: 1, MediaSSRC: 2},
	})
	if err != nil {
		t.Fatal(err)
	}

	rig.aux.HandleSIPRTCP(raw)

	if len(got) != 1 {
		t.Fatalf("browser RTCP packets = %d, want 1", len(got))
	}
	if pli, ok := got[0].(*rtcp.PictureLossIndication); !ok || pli.MediaSSRC != 0xCAFE {
		t.Fatalf("got %#v, want PLI for the auxiliary track", got[0])
	}
}

func auxDownlinkPacket(seq uint16, payload []byte) []byte {
	packet := rtp.Packet{
		Header:  rtp.Header{Version: 2, PayloadType: 99, SequenceNumber: seq, Timestamp: 9000, SSRC: 0x51515151},
		Payload: payload,
	}
	raw, _ := packet.Marshal()
	return raw
}

func newAuxLocalTrack(t *testing.T) *webrtc.TrackLocalStaticRTP {
	t.Helper()
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000}, "screen", "aux-test")
	if err != nil {
		t.Fatal(err)
	}
	return track
}

func TestAuxDownlinkDropsUntilTrackThenAsksSIPForKeyframe(t *testing.T) {
	rig := newAuxTestRig(t)

	rig.aux.HandleSIPRTP(auxDownlinkPacket(10, auxTestIDR))
	if stats := rig.aux.Stats(); stats.DownlinkWritten != 0 || stats.DownlinkDropped != 1 {
		t.Fatalf("stats before track = %+v, want one dropped", stats)
	}

	rig.aux.SetLocalTrack(newAuxLocalTrack(t))

	packets := readRTCP(t, rig.peerRTCP)
	if len(packets) != 2 {
		t.Fatalf("SIP RTCP packets = %d, want RR + PLI", len(packets))
	}
	pli, ok := packets[1].(*rtcp.PictureLossIndication)
	if !ok || pli.MediaSSRC != 0x51515151 {
		t.Fatalf("got %#v, want PLI for the peer's auxiliary SSRC", packets[1])
	}
	if pli.SenderSSRC == 0 || pli.SenderSSRC == 0x51515151 {
		t.Fatalf("PLI sender SSRC = %d, want the auxiliary uplink SSRC", pli.SenderSSRC)
	}

	rig.aux.HandleSIPRTP(auxDownlinkPacket(11, auxTestP))
	rig.aux.HandleSIPRTP(auxDownlinkPacket(12, auxTestP))
	if stats := rig.aux.Stats(); stats.DownlinkWritten != 2 {
		t.Fatalf("stats after track = %+v, want two written", stats)
	}
}

func TestAuxBrowserFeedbackUsesAuxiliaryRTCPPathAndHistory(t *testing.T) {
	rig := newAuxTestRig(t)
	rig.aux.SetLocalTrack(newAuxLocalTrack(t))
	rig.aux.HandleSIPRTP(auxDownlinkPacket(20, auxTestIDR))
	rig.aux.HandleSIPRTP(auxDownlinkPacket(21, auxTestP))
	_ = readRTCP(t, rig.peerRTCP) // keyframe request sent when the SSRC was learned

	// 21 is in the history and is resent to the browser. 300 was never
	// received, so it is NACKed on to the SIP side.
	rig.aux.HandleBrowserRTCP([]rtcp.Packet{&rtcp.TransportLayerNack{
		Nacks: []rtcp.NackPair{{PacketID: 21}, {PacketID: 300}},
	}})

	if stats := rig.aux.Stats(); stats.NACKRetransmits != 1 || stats.NACKMisses != 1 {
		t.Fatalf("stats = %+v, want 1 retransmit and 1 miss", stats)
	}
	packets := readRTCP(t, rig.peerRTCP)
	if len(packets) != 2 {
		t.Fatalf("SIP RTCP packets = %d, want RR + NACK", len(packets))
	}
	nack, ok := packets[1].(*rtcp.TransportLayerNack)
	if !ok || nack.MediaSSRC != 0x51515151 || len(nack.Nacks) != 1 || nack.Nacks[0].PacketID != 300 {
		t.Fatalf("got %#v, want NACK for 300 on the peer's auxiliary SSRC", packets[1])
	}
}

func TestAuxSIPRTCPUsesRTPPortWithRTCPMux(t *testing.T) {
	rig := newAuxTestRig(t)
	rig.aux.SetRemoteEndpoints(udpAddrOf(rig.peerRTP), nil, true)
	rig.aux.SetLocalTrack(newAuxLocalTrack(t))

	rig.aux.HandleSIPRTP(auxDownlinkPacket(1, auxTestIDR))

	if packets := readRTCP(t, rig.peerRTP); len(packets) != 2 {
		t.Fatalf("expected RR + PLI on the peer's RTP port with rtcp-mux, got %d packets", len(packets))
	}
}

func TestAuxWriteErrorsAreCountedAndDoNotPanic(t *testing.T) {
	rig := newAuxTestRig(t)
	rig.aux.HandleUplinkRTP(browserPacket(1, 1000, false, auxTestSPS))
	rig.aux.HandleUplinkRTP(browserPacket(2, 1000, false, auxTestPPS))
	// Close the socket under the stream, as a failing network path would.
	rig.aux.mu.Lock()
	conn := rig.aux.rtpConn
	rig.aux.mu.Unlock()
	conn.Close()

	rig.aux.HandleUplinkRTP(browserPacket(3, 1000, true, auxTestIDR))

	if stats := rig.aux.Stats(); stats.UplinkWriteErrs == 0 {
		t.Fatalf("stats = %+v, want write errors counted", stats)
	}
}

func TestAuxVideoOpenCloseLifecycle(t *testing.T) {
	sess := &Session{ID: "aux-lifecycle", RTPBufferSize: 2048, State: StateActive}
	rtpConn := listenLoopbackUDP(t)
	rtcpConn := listenLoopbackUDP(t)
	port := udpAddrOf(rtpConn).Port

	aux, err := sess.OpenAuxVideo(rtpConn, rtcpConn, port, udpAddrOf(rtcpConn).Port)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.OpenAuxVideo(listenLoopbackUDP(t), listenLoopbackUDP(t), 1, 2); err == nil {
		t.Fatal("a second auxiliary stream must be refused")
	}
	if sess.AuxVideo() != aux {
		t.Fatal("AuxVideo must return the open stream")
	}

	sess.CloseMediaTransports()

	if sess.AuxVideo() != nil {
		t.Fatal("media teardown must close the auxiliary stream")
	}
	if sess.CloseAuxVideo("again") {
		t.Fatal("closing twice must report nothing was open")
	}
	rebound, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatalf("auxiliary RTP port was not released: %v", err)
	}
	rebound.Close()

	// A closed stream ignores late packets and requests.
	aux.HandleUplinkRTP(browserPacket(1, 1, true, auxTestIDR))
	aux.HandleSIPRTP(auxDownlinkPacket(1, auxTestIDR))
	if aux.RequestKeyframeFromSIP("late") || aux.RequestKeyframeFromBrowser("late") {
		t.Fatal("a closed stream must not send keyframe requests")
	}
}

// queuedRTPReader feeds marshalled RTP packets to a forwarder, then reports
// the track as closed.
type queuedRTPReader struct {
	packets [][]byte
}

func (r *queuedRTPReader) Read(b []byte) (int, error) {
	if len(r.packets) == 0 {
		return 0, io.EOF
	}
	n := copy(b, r.packets[0])
	r.packets = r.packets[1:]
	return n, nil
}

func marshalBrowserPackets(t *testing.T, packets ...*rtp.Packet) [][]byte {
	t.Helper()
	var out [][]byte
	for _, packet := range packets {
		packet.Extension = false
		packet.Extensions = nil
		raw, err := packet.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, raw)
	}
	return out
}

func TestCameraAndAuxUplinkStayIndependent(t *testing.T) {
	rig := newAuxTestRig(t)
	cameraPeer := listenLoopbackUDP(t)
	cameraSPS := []byte{0x67, 0x42, 0xe0, 0x1e, 0xaa}
	cameraPPS := []byte{0x68, 0xce, 0x01, 0x02}
	rig.sess.mediaForwardReady = true
	rig.sess.VideoRTPConn = listenLoopbackUDP(t)
	rig.sess.AsteriskVideoAddr = udpAddrOf(cameraPeer)
	rig.sess.CachedSPS = cameraSPS
	rig.sess.CachedPPS = cameraPPS

	cameraDone := make(chan struct{})
	go func() {
		defer close(cameraDone)
		rig.sess.forwardRTPToAsteriskFrom(&queuedRTPReader{packets: marshalBrowserPackets(t,
			browserPacket(1, 1000, true, []byte{0x65, 0xc0, 0xff, 0xee}),
		)}, "video")
	}()
	rig.sess.forwardAuxUplink(&queuedRTPReader{packets: marshalBrowserPackets(t,
		browserPacket(1, 5000, false, auxTestSPS),
		browserPacket(2, 5000, false, auxTestPPS),
		browserPacket(3, 5000, true, auxTestIDR),
	)}, 0xCAFE, nil)
	select {
	case <-cameraDone:
	case <-time.After(5 * time.Second):
		t.Fatal("camera forwarder did not return")
	}

	auxPackets := readRTP(t, rig.peerRTP)
	cameraPackets := readRTP(t, cameraPeer)
	assertPayloads(t, auxPackets, auxTestSPS, auxTestPPS, auxTestIDR)
	if len(cameraPackets) == 0 {
		t.Fatal("camera packets did not reach the camera's SIP port")
	}
	for _, packet := range cameraPackets {
		if packet.SSRC == auxPackets[0].SSRC {
			t.Fatal("camera and auxiliary streams share an SSRC")
		}
		if bytes.Equal(packet.Payload, auxTestSPS) || bytes.Equal(packet.Payload, auxTestPPS) || bytes.Equal(packet.Payload, auxTestIDR) {
			t.Fatalf("auxiliary payload %x reached the camera's SIP port", packet.Payload)
		}
	}
	if !bytes.Equal(rig.sess.CachedSPS, cameraSPS) || !bytes.Equal(rig.sess.CachedPPS, cameraPPS) {
		t.Fatalf("auxiliary parameter sets leaked into the camera cache: sps=%x pps=%x", rig.sess.CachedSPS, rig.sess.CachedPPS)
	}
	if rig.sess.VideoSSRC == 0 || rig.sess.VideoSSRC == auxPackets[0].SSRC {
		t.Fatalf("camera SSRC = %d, auxiliary SSRC = %d", rig.sess.VideoSSRC, auxPackets[0].SSRC)
	}
}

func TestForwardAuxUplinkDiscardsWhileNoStreamIsOpen(t *testing.T) {
	sess := &Session{ID: "aux-idle", RTPBufferSize: 2048, State: StateActive}

	done := make(chan struct{})
	go func() {
		defer close(done)
		sess.forwardAuxUplink(&queuedRTPReader{packets: marshalBrowserPackets(t,
			browserPacket(1, 1000, true, auxTestIDR),
		)}, 0xCAFE, nil)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("forwarder must drain and return when the track closes")
	}
}

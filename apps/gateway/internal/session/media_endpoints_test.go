package session

import (
	"net"
	"testing"
	"time"
)

func newUDPConn(t *testing.T) (*net.UDPConn, int) {
	t.Helper()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 0,
	})
	if err != nil {
		t.Fatalf("failed to open UDP listener: %v", err)
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	return conn, port
}

func assertUDPConnClosed(t *testing.T, conn *net.UDPConn) {
	t.Helper()
	if conn == nil {
		t.Fatalf("nil conn")
	}
	addr := conn.LocalAddr().(*net.UDPAddr)
	_ = conn.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := conn.WriteToUDP([]byte{0x01}, addr); err == nil {
		t.Fatalf("expected closed UDP conn write to fail")
	}
}

func TestCloseMediaTransportsClearsAndCloses(t *testing.T) {
	audioRTP, audioRTPPort := newUDPConn(t)
	videoRTP, videoRTPPort := newUDPConn(t)
	audioRTCP, audioRTCPPort := newUDPConn(t)
	videoRTCP, videoRTCPPort := newUDPConn(t)

	sess := &Session{
		ID:            "test-close-media",
		RTPConn:       audioRTP,
		VideoRTPConn:  videoRTP,
		AudioRTCPConn: audioRTCP,
		VideoRTCPConn: videoRTCP,
		RTPPort:       audioRTPPort,
		VideoRTPPort:  videoRTPPort,
		AudioRTCPPort: audioRTCPPort,
		VideoRTCPPort: videoRTCPPort,
	}

	sess.CloseMediaTransports()

	status := sess.GetMediaEndpointStatus()
	if status.AudioRTPReady || status.VideoRTPReady || status.AudioRTCPReady || status.VideoRTCPReady {
		t.Fatalf("expected all RTP/RTCP transports to be cleared, got %+v", status)
	}
	if status.AudioRTPPort != 0 || status.VideoRTPPort != 0 || status.AudioRTCPPort != 0 || status.VideoRTCPPort != 0 {
		t.Fatalf("expected media ports to be reset, got %+v", status)
	}

	assertUDPConnClosed(t, audioRTP)
	assertUDPConnClosed(t, videoRTP)
	assertUDPConnClosed(t, audioRTCP)
	assertUDPConnClosed(t, videoRTCP)
}

func TestResetMediaStateClearsSIPEndpointsKeepsCachedSPSPPS(t *testing.T) {
	cachedSPS := []byte{0x67, 0x42, 0x00, 0x1f}
	cachedPPS := []byte{0x68, 0xce, 0x06, 0xe2}

	sess := &Session{
		ID: "test-reset-media",
		AsteriskAudioAddr: &net.UDPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: 4000,
		},
		AsteriskVideoAddr: &net.UDPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: 4002,
		},
		CachedSPS: cachedSPS,
		CachedPPS: cachedPPS,
		AudioSeq:  111,
		VideoSeq:  222,
	}

	sess.ResetMediaState()

	if sess.AsteriskAudioAddr != nil {
		t.Fatalf("expected AsteriskAudioAddr to be cleared")
	}
	if sess.AsteriskVideoAddr != nil {
		t.Fatalf("expected AsteriskVideoAddr to be cleared")
	}
	if len(sess.CachedSPS) == 0 || len(sess.CachedPPS) == 0 {
		t.Fatalf("expected cached SPS/PPS to be preserved")
	}
	if string(sess.CachedSPS) != string(cachedSPS) {
		t.Fatalf("cached SPS changed unexpectedly")
	}
	if string(sess.CachedPPS) != string(cachedPPS) {
		t.Fatalf("cached PPS changed unexpectedly")
	}
}

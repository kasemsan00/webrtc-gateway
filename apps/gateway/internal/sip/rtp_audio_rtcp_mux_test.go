package sip

import (
	"net"
	"testing"
	"time"

	"github.com/pion/rtcp"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

// With a=rtcp-mux, Asterisk's sender reports arrive on the audio RTP port.
// They must be counted as RTCP, not treated as audio.
func TestAudioRTPLoop_CountsMuxedRTCP(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{rtpConfig: config.RTPConfig{BufferSize: 1500}}
	sess := &session.Session{ID: "audio-rtcp-mux"}
	done := make(chan struct{})
	go func() {
		srv.handleAudioRTPPacketsForSession(conn, sess)
		close(done)
	}()

	sender, err := net.DialUDP("udp4", nil, conn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	sr, _ := rtcp.Marshal([]rtcp.Packet{&rtcp.SenderReport{SSRC: 42}})
	if _, err := sender.Write(sr); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for sess.AudioRTCPSRCount.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("muxed SR was not counted as RTCP")
		}
		time.Sleep(5 * time.Millisecond)
	}
	sess.SetState(session.StateEnded)
	conn.Close()
	<-done
}

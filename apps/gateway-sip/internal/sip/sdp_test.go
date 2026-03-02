package sip

import (
	"strings"
	"testing"

	"k2-gateway/internal/config"
	"k2-gateway/internal/session"
)

func TestCreateSDPOffer_IncludesRTCPMuxForAudioAndVideo(t *testing.T) {
	s := &Server{
		config:        config.SIPConfig{},
		publicAddress: "203.0.113.10",
	}
	sess := &session.Session{ID: "test-rtcp-mux"}

	offer := string(s.createSDPOffer(12000, sess))
	if got := strings.Count(offer, "a=rtcp-mux"); got != 2 {
		t.Fatalf("expected 2 rtcp-mux lines (audio+video), got %d\nSDP:\n%s", got, offer)
	}
}

func TestCreateSDPOffer_AVPFStillIncludesRTCPMux(t *testing.T) {
	s := &Server{
		config: config.SIPConfig{
			AudioUseAVPF: true,
			VideoUseAVPF: true,
		},
		publicAddress: "203.0.113.10",
	}
	sess := &session.Session{ID: "test-avpf-rtcp-mux"}

	offer := string(s.createSDPOffer(12000, sess))

	if !strings.Contains(offer, "m=audio 12000 RTP/AVPF 111 101") {
		t.Fatalf("expected audio AVPF profile in SDP\nSDP:\n%s", offer)
	}
	if !strings.Contains(offer, "m=video 12002 RTP/AVPF 96") {
		t.Fatalf("expected video AVPF profile in SDP\nSDP:\n%s", offer)
	}
	if got := strings.Count(offer, "a=rtcp-mux"); got != 2 {
		t.Fatalf("expected 2 rtcp-mux lines (audio+video), got %d\nSDP:\n%s", got, offer)
	}
	if !strings.Contains(offer, "a=rtcp-fb:* ccm fir") {
		t.Fatalf("expected rtcp-fb line in AVPF mode\nSDP:\n%s", offer)
	}
}


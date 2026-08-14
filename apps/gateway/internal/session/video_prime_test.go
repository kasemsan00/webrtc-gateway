package session

import (
	"context"
	"testing"
	"time"
)

func TestShouldPrimeVideoForSIPOffer_SkipsWhenNoUplink(t *testing.T) {
	if ShouldPrimeVideoForSIPOffer("v=0\r\nm=video 9 RTP/AVP 96\r\na=recvonly\r\n") {
		t.Fatalf("recvonly must skip PrimeWebRTCVideoForSIPOffer")
	}
	if ShouldPrimeVideoForSIPOffer("v=0\r\nm=audio 9 RTP/AVP 111\r\na=sendrecv\r\n") {
		t.Fatalf("audio-only must skip PrimeWebRTCVideoForSIPOffer")
	}
}

func TestPrimeWebRTCVideoForSIPOffer_ReturnsReadyWhenSPSPPSCached(t *testing.T) {
	sess := &Session{
		ID:        "prime-ready",
		CachedSPS: []byte{0x67, 0x42, 0x00, 0x1f},
		CachedPPS: []byte{0x68, 0xce, 0x06, 0xe2},
	}

	if !sess.PrimeWebRTCVideoForSIPOffer(context.Background(), time.Second) {
		t.Fatalf("expected cached SPS/PPS to satisfy video priming immediately")
	}
}

func TestPrimeWebRTCVideoForSIPOffer_TimesOutWhenSPSPPSMissing(t *testing.T) {
	sess := &Session{ID: "prime-timeout"}

	if sess.PrimeWebRTCVideoForSIPOffer(context.Background(), 10*time.Millisecond) {
		t.Fatalf("expected video priming to timeout without SPS/PPS")
	}
}

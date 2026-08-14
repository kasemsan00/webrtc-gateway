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

func TestCreateSDPOffer_IncludesCachedSpropParameterSets(t *testing.T) {
	s := &Server{
		config:        config.SIPConfig{},
		publicAddress: "203.0.113.10",
	}
	sess := &session.Session{
		ID:        "test-sprop",
		CachedSPS: []byte{0x67, 0x42, 0xe0, 0x1f},
		CachedPPS: []byte{0x68, 0xce, 0x06, 0xe2},
	}

	offer := string(s.createSDPOffer(12000, sess))

	if !strings.Contains(offer, "sprop-parameter-sets=Z0LgHw==,aM4G4g==") {
		t.Fatalf("expected cached SPS/PPS in SDP fmtp\nSDP:\n%s", offer)
	}
	if !strings.Contains(offer, "profile-level-id=42E01F") {
		t.Fatalf("expected profile-level-id derived from cached SPS\nSDP:\n%s", offer)
	}
}

func TestCreateSDPAnswerForInvite_HonorsPacketizationMode0Default(t *testing.T) {
	s := &Server{config: config.SIPConfig{}, publicAddress: "203.0.113.10"}
	sess := &session.Session{ID: "test-mode0"}
	invite := []byte("v=0\r\nm=audio 4000 RTP/AVP 107\r\na=rtpmap:107 opus/48000/2\r\nm=video 4002 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=fmtp:96 profile-level-id=42801F\r\na=sendrecv\r\n")

	answer := string(s.createSDPAnswerForInvite(12000, sess, invite))
	if !strings.Contains(answer, "profile-level-id=42E01F;packetization-mode=0") {
		t.Fatalf("expected packetization-mode=0 in answer\nSDP:\n%s", answer)
	}
	if strings.Contains(answer, "packetization-mode=1") {
		t.Fatalf("unexpected packetization-mode=1 in answer\nSDP:\n%s", answer)
	}
	if got := sess.GetSIPVideoPacketizationMode(); got != 0 {
		t.Fatalf("session packetization mode = %d, want 0", got)
	}
}

func TestCreateSDPAnswerForInvite_PreservesOfferedPacketizationMode1(t *testing.T) {
	s := &Server{config: config.SIPConfig{}, publicAddress: "203.0.113.10"}
	sess := &session.Session{ID: "test-mode1"}
	invite := []byte("v=0\r\nm=audio 4000 RTP/AVP 107\r\na=rtpmap:107 opus/48000/2\r\nm=video 4002 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=fmtp:96 profile-level-id=42E01F;packetization-mode=1\r\na=sendrecv\r\n")

	answer := string(s.createSDPAnswerForInvite(12000, sess, invite))
	if !strings.Contains(answer, "profile-level-id=42E01F;packetization-mode=1") {
		t.Fatalf("expected packetization-mode=1 in answer\nSDP:\n%s", answer)
	}
	if got := sess.GetSIPVideoPacketizationMode(); got != 1 {
		t.Fatalf("session packetization mode = %d, want 1", got)
	}
}

func TestCreateSDPOffer_OmitsVideoWhenFlagFalse(t *testing.T) {
	s := &Server{
		config:        config.SIPConfig{},
		publicAddress: "203.0.113.10",
	}
	sess := &session.Session{ID: "test-audio-only"}
	sess.SetSIPOfferIncludeVideo(false)

	offer := string(s.createSDPOffer(12000, sess))
	if strings.Contains(offer, "m=video") {
		t.Fatalf("audio-only SIP offer must omit m=video\nSDP:\n%s", offer)
	}
	if got := strings.Count(offer, "a=rtcp-mux"); got != 1 {
		t.Fatalf("expected 1 rtcp-mux line (audio only), got %d\nSDP:\n%s", got, offer)
	}
	if !strings.Contains(offer, "m=audio 12000 RTP/AVP 111 101") {
		t.Fatalf("expected audio m-line in audio-only SDP\nSDP:\n%s", offer)
	}
}

func TestCreateSDPAnswerForInvite_KeepsVideoWhenInviteHasVideo(t *testing.T) {
	s := &Server{config: config.SIPConfig{}, publicAddress: "203.0.113.10"}
	sess := &session.Session{ID: "test-inbound-video"}
	invite := []byte("v=0\r\nm=audio 4000 RTP/AVP 107\r\na=rtpmap:107 opus/48000/2\r\nm=video 4002 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=fmtp:96 profile-level-id=42E01F;packetization-mode=1\r\na=sendrecv\r\n")

	answer := string(s.createSDPAnswerForInvite(12000, sess, invite))
	if !strings.Contains(answer, "m=video 12002 RTP/AVP 96") {
		t.Fatalf("inbound video INVITE must keep m=video in the answer\nSDP:\n%s", answer)
	}
	if !sess.SIPOfferIncludeVideo() {
		t.Fatalf("session must keep SIPOfferIncludeVideo after a video INVITE")
	}
}

func TestCreateSDPAnswerForInvite_OmitsVideoWhenInviteIsAudioOnly(t *testing.T) {
	s := &Server{config: config.SIPConfig{}, publicAddress: "203.0.113.10"}
	sess := &session.Session{ID: "test-inbound-audio"}
	invite := []byte("v=0\r\nm=audio 4000 RTP/AVP 107\r\na=rtpmap:107 opus/48000/2\r\na=sendrecv\r\n")

	answer := string(s.createSDPAnswerForInvite(12000, sess, invite))
	if strings.Contains(answer, "m=video") {
		t.Fatalf("audio-only INVITE must not get m=video in the answer\nSDP:\n%s", answer)
	}
	if sess.SIPOfferIncludeVideo() {
		t.Fatalf("audio-only INVITE must clear SIPOfferIncludeVideo")
	}
}

func TestSDPFmtpHasParameter(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{line: "a=fmtp:103 level-asymmetry-allowed=1; packetization-mode=1;profile-level-id=42e01f", want: true},
		{line: "a=fmtp:96 profile-level-id=42801F;packetization-mode=0", want: false},
		{line: "a=fmtp:96 packetization-mode=10", want: false},
		{line: "a=rtpmap:96 H264/90000", want: false},
	}
	for _, tt := range tests {
		if got := sdpFmtpHasParameter(tt.line, "packetization-mode", "1"); got != tt.want {
			t.Errorf("sdpFmtpHasParameter(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestSIPVideoPacketizationModeDefaultsToZero(t *testing.T) {
	sdp := []byte("v=0\r\nm=video 4002 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=fmtp:96 profile-level-id=42801F\r\n")
	if got := sipVideoPacketizationMode(sdp); got != 0 {
		t.Fatalf("packetization mode = %d, want 0", got)
	}
}

func TestSIPVideoPacketizationModeFindsModeOneOnDynamicPayload(t *testing.T) {
	sdp := []byte("v=0\r\nm=video 4002 RTP/AVP 103\r\na=rtpmap:103 H264/90000\r\na=fmtp:103 profile-level-id=42e01f; packetization-mode=1\r\nm=audio 4000 RTP/AVP 111\r\n")
	if got := sipVideoPacketizationMode(sdp); got != 1 {
		t.Fatalf("packetization mode = %d, want 1", got)
	}
}

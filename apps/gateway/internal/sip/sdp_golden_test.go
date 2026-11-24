package sip

import (
	"regexp"
	"strings"
	"testing"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

// These tests pin the exact SDP bytes the gateway produced for calls with one
// audio and at most one video stream before multi-video SDP handling was
// added. Only the time-based o= identifiers are normalized. A diff here is a
// wire change for every existing call and must be intentional.

var sdpOriginIDs = regexp.MustCompile(`(?m)^o=(\S+) \d+ \d+ `)

func normalizeSDPOrigin(sdp []byte) string {
	return sdpOriginIDs.ReplaceAllString(string(sdp), "o=$1 1 1 ")
}

const (
	goldenOfferAVP = "v=0\no=- 1 1 IN IP4 203.0.113.10\ns=Talk\nc=IN IP4 203.0.113.10\nt=0 0\n" +
		"m=audio 12000 RTP/AVP 111 101\na=rtpmap:111 opus/48000/2\na=fmtp:111 minptime=10;useinbandfec=1\n" +
		"a=rtpmap:101 telephone-event/8000\na=fmtp:101 0-16\na=ptime:20\na=rtcp-mux\na=sendrecv\n" +
		"m=video 12002 RTP/AVP 96\na=rtpmap:96 H264/90000\na=fmtp:96 profile-level-id=42E01F;packetization-mode=1\n" +
		"a=rtcp-mux\na=sendrecv\n"

	goldenOfferAVPF = "v=0\no=- 1 1 IN IP4 203.0.113.10\ns=Talk\nc=IN IP4 203.0.113.10\nt=0 0\n" +
		"m=audio 12000 RTP/AVPF 111 101\na=rtpmap:111 opus/48000/2\na=fmtp:111 minptime=10;useinbandfec=1\n" +
		"a=rtpmap:101 telephone-event/8000\na=fmtp:101 0-16\na=ptime:20\na=rtcp-mux\na=sendrecv\n" +
		"m=video 12002 RTP/AVPF 96\na=rtpmap:96 H264/90000\na=fmtp:96 profile-level-id=42E01F;packetization-mode=1\n" +
		"a=rtcp-mux\na=rtcp-fb:* ccm fir\na=rtcp-fb:* nack\na=rtcp-fb:* nack pli\na=sendrecv\n"

	goldenOfferAudioOnly = "v=0\no=- 1 1 IN IP4 203.0.113.10\ns=Talk\nc=IN IP4 203.0.113.10\nt=0 0\n" +
		"m=audio 12000 RTP/AVP 111 101\na=rtpmap:111 opus/48000/2\na=fmtp:111 minptime=10;useinbandfec=1\n" +
		"a=rtpmap:101 telephone-event/8000\na=fmtp:101 0-16\na=ptime:20\na=rtcp-mux\na=sendrecv\n"

	goldenAnswerAVP = "v=0\no=- 1 1 IN IP4 203.0.113.10\ns=Talk\nc=IN IP4 203.0.113.10\nt=0 0\n" +
		"m=audio 4000 RTP/AVP 111 101\na=rtpmap:111 opus/48000/2\na=fmtp:111 minptime=10;useinbandfec=1\n" +
		"a=rtpmap:101 telephone-event/8000\na=fmtp:101 0-16\na=ptime:20\na=sendrecv\n" +
		"m=video 4002 RTP/AVP 99\na=rtcp:4003\na=rtpmap:99 H264/90000\na=fmtp:99 profile-level-id=42E01F;packetization-mode=1\n" +
		"a=sendrecv\n"

	goldenAnswerAudioOnly = "v=0\no=- 1 1 IN IP4 203.0.113.10\ns=Talk\nc=IN IP4 203.0.113.10\nt=0 0\n" +
		"m=audio 4000 RTP/AVP 111 101\na=rtpmap:111 opus/48000/2\na=fmtp:111 minptime=10;useinbandfec=1\n" +
		"a=rtpmap:101 telephone-event/8000\na=fmtp:101 0-16\na=ptime:20\na=sendrecv\n"

	goldenInviteAudioVideo = "v=0\r\no=- 1 1 IN IP4 198.51.100.7\r\ns=-\r\nc=IN IP4 198.51.100.7\r\nt=0 0\r\n" +
		"m=audio 4000 RTP/AVP 107 101\r\na=rtpmap:107 opus/48000/2\r\na=rtpmap:101 telephone-event/8000\r\na=sendrecv\r\n" +
		"m=video 4002 RTP/AVP 99\r\na=rtpmap:99 H264/90000\r\na=fmtp:99 profile-level-id=42801F;packetization-mode=1\r\na=sendrecv\r\n"

	goldenInviteAudioOnly = "v=0\r\no=- 1 1 IN IP4 198.51.100.7\r\ns=-\r\nc=IN IP4 198.51.100.7\r\nt=0 0\r\n" +
		"m=audio 4000 RTP/AVP 107 101\r\na=rtpmap:107 opus/48000/2\r\na=sendrecv\r\n"
)

func assertGoldenSDP(t *testing.T, name string, got []byte, want string) {
	t.Helper()
	if normalized := normalizeSDPOrigin(got); normalized != want {
		t.Fatalf("%s SDP changed\n got: %q\nwant: %q", name, normalized, want)
	}
}

func TestGoldenSDPOffer(t *testing.T) {
	avp := &Server{config: config.SIPConfig{}, publicAddress: "203.0.113.10"}
	avpf := &Server{config: config.SIPConfig{AudioUseAVPF: true, VideoUseAVPF: true}, publicAddress: "203.0.113.10"}

	assertGoldenSDP(t, "offer AVP", avp.createSDPOffer(12000, &session.Session{ID: "golden-offer-avp"}), goldenOfferAVP)
	assertGoldenSDP(t, "offer AVPF", avpf.createSDPOffer(12000, &session.Session{ID: "golden-offer-avpf"}), goldenOfferAVPF)

	audioOnly := &session.Session{ID: "golden-offer-audio"}
	audioOnly.SetSIPOfferIncludeVideo(false)
	assertGoldenSDP(t, "offer audio-only", avp.createSDPOffer(12000, audioOnly), goldenOfferAudioOnly)
}

func TestGoldenSDPAnswerForInvite(t *testing.T) {
	srv := &Server{config: config.SIPConfig{AudioUseAVPF: true, VideoUseAVPF: true}, publicAddress: "203.0.113.10"}

	assertGoldenSDP(t, "answer audio+video",
		srv.createSDPAnswerForInvite(4000, &session.Session{ID: "golden-answer-av"}, []byte(goldenInviteAudioVideo)),
		goldenAnswerAVP)
	assertGoldenSDP(t, "answer audio-only",
		srv.createSDPAnswerForInvite(4000, &session.Session{ID: "golden-answer-audio"}, []byte(goldenInviteAudioOnly)),
		goldenAnswerAudioOnly)
}

func TestGoldenSDPHoldRequest(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{Domain: "203.0.113.20", Port: 5060},
		publicAddress: "203.0.113.10",
		sipPort:       5090,
	}
	sess := &session.Session{ID: "golden-hold"}
	sess.SetCallInfo("outbound", "sip:1001@203.0.113.20:5060", "sip:2002@203.0.113.20:5060", "call-golden-hold")
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:2002@203.0.113.20:5060>", "203.0.113.20", 5060, 7, nil)
	sess.SetRTPConnection(nil, 12000)

	hold, err := srv.createHoldRequest(sess, true)
	if err != nil {
		t.Fatalf("createHoldRequest hold: %v", err)
	}
	assertGoldenSDP(t, "hold", hold.Body(), strings.ReplaceAll(goldenOfferAVP, "a=sendrecv", "a=inactive"))

	resume, err := srv.createHoldRequest(sess, false)
	if err != nil {
		t.Fatalf("createHoldRequest resume: %v", err)
	}
	assertGoldenSDP(t, "resume", resume.Body(), goldenOfferAVP)
}

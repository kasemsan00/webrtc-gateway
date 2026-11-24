package sip

import (
	"net"
	"strings"
	"testing"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

// A re-INVITE that keeps the camera on 20002 and opens a second video stream
// on 20006. The second stream carries its own c= line, transport, payload
// type and rtcp-mux so a parser that lets a later m=video win is caught.
const offerWithSecondVideo = "v=0\r\no=- 1 2 IN IP4 10.0.0.5\r\ns=-\r\nc=IN IP4 10.0.0.5\r\nt=0 0\r\n" +
	"m=audio 20000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\na=sendrecv\r\n" +
	"m=video 20002 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=fmtp:96 profile-level-id=42E01F;packetization-mode=0\r\na=sendrecv\r\n" +
	"m=video 20006 RTP/AVPF 100\r\nc=IN IP4 10.0.0.77\r\na=rtpmap:100 H264/90000\r\na=fmtp:100 profile-level-id=42E01F;packetization-mode=1\r\na=rtcp-mux\r\na=rtcp-fb:* nack pli\r\na=content:slides\r\na=sendonly\r\n"

func sdpMediaLines(sdp []byte) []string {
	var lines []string
	for _, raw := range strings.Split(string(sdp), "\n") {
		if line := strings.TrimSpace(raw); strings.HasPrefix(line, "m=") {
			lines = append(lines, line)
		}
	}
	return lines
}

func assertMediaLines(t *testing.T, sdp []byte, want ...string) {
	t.Helper()
	got := sdpMediaLines(sdp)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("m-lines = %q, want %q\nSDP:\n%s", got, want, sdp)
	}
}

func TestParseSDPMediaEndpoints_SecondVideoDoesNotMoveCamera(t *testing.T) {
	ip, audioPort, videoPort, videoProfile := parseSDPMediaEndpoints(strings.Split(offerWithSecondVideo, "\n"))

	if ip != "10.0.0.5" || audioPort != 20000 || videoPort != 20002 || videoProfile != "RTP/AVP" {
		t.Fatalf("primary endpoints = %s audio=%d video=%d profile=%s, want 10.0.0.5 20000 20002 RTP/AVP",
			ip, audioPort, videoPort, videoProfile)
	}
}

func TestPrimaryVideoParsersIgnoreSecondVideo(t *testing.T) {
	body := []byte(offerWithSecondVideo)

	if pt, ok := parseH264PayloadType(body); !ok || pt != 96 {
		t.Fatalf("H264 payload type = %d ok=%v, want the camera's 96", pt, ok)
	}
	if mode := sipVideoPacketizationMode(body); mode != 0 {
		t.Fatalf("packetization mode = %d, want the camera's 0", mode)
	}
	if sdpVideoHasRTCPFeedback(offerWithSecondVideo) {
		t.Fatal("rtcp-fb on the second video must not be reported for the camera")
	}
}

func TestCreateSDPAnswerForInvite_SecondVideoDoesNotChangePrimaryAnswer(t *testing.T) {
	srv := &Server{config: config.SIPConfig{AudioUseAVPF: true, VideoUseAVPF: true}, publicAddress: "203.0.113.10"}
	withSecond := srv.createSDPAnswerForInvite(4000, &session.Session{ID: "second-video"}, []byte(offerWithSecondVideo))

	cameraOnly := offerWithSecondVideo[:strings.Index(offerWithSecondVideo, "m=video 20006")]
	plain := srv.createSDPAnswerForInvite(4000, &session.Session{ID: "camera-only"}, []byte(cameraOnly))

	if normalizeSDPOrigin(withSecond) != normalizeSDPOrigin(plain) {
		t.Fatalf("second m=video changed the primary answer\n got: %q\nwant: %q",
			normalizeSDPOrigin(withSecond), normalizeSDPOrigin(plain))
	}
}

func TestRenderSDPAnswerForLayout_PlainOrderIsByteIdentical(t *testing.T) {
	for name, tc := range map[string]struct{ offer, base string }{
		"audio+video": {goldenInviteAudioVideo, goldenAnswerAVP},
		"audio-only":  {goldenInviteAudioOnly, goldenAnswerAudioOnly},
	} {
		layout := sipMediaLayoutFromOffer([]byte(tc.offer))
		if got := string(renderSDPAnswerForLayout([]byte(tc.base), layout, nil)); got != tc.base {
			t.Fatalf("%s: answer changed\n got: %q\nwant: %q", name, got, tc.base)
		}
		if got := string(renderSDPOfferForLayout([]byte(tc.base), layout, nil)); got != tc.base {
			t.Fatalf("%s: offer changed\n got: %q\nwant: %q", name, got, tc.base)
		}
	}
	if got := string(renderSDPAnswerForLayout([]byte(goldenAnswerAVP), nil, nil)); got != goldenAnswerAVP {
		t.Fatalf("nil layout must leave the SDP untouched, got %q", got)
	}
}

func TestRenderSDPAnswerForLayout_DeclinesUnusedMLinesInPlace(t *testing.T) {
	offer := "v=0\r\nm=audio 20000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\n" +
		"m=application 20010 UDP/BFCP *\r\na=setup:actpass\r\n" +
		"m=video 20002/2 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\n" +
		"m=audio 20020 RTP/AVP 0\r\n" +
		"m=video 20006 RTP/AVPF 100 101\r\na=rtpmap:100 H264/90000\r\n"

	answer := renderSDPAnswerForLayout([]byte(goldenAnswerAVP), sipMediaLayoutFromOffer([]byte(offer)), nil)

	assertMediaLines(t, answer,
		"m=audio 4000 RTP/AVP 111 101",
		"m=application 0 UDP/BFCP *",
		"m=video 4002 RTP/AVP 99",
		"m=audio 0 RTP/AVP 0",
		"m=video 0 RTP/AVPF 100 101",
	)
	if !strings.Contains(string(answer), "m=video 4002 RTP/AVP 99\na=rtcp:4003\n") {
		t.Fatalf("the camera section must keep its attributes\nSDP:\n%s", answer)
	}
	if strings.Contains(string(answer), "a=setup") {
		t.Fatalf("a declined m-line must not carry the offer's attributes\nSDP:\n%s", answer)
	}
	if !strings.HasSuffix(string(answer), "\n") || strings.HasSuffix(string(answer), "\n\n") {
		t.Fatalf("answer must end with exactly one newline: %q", answer)
	}
}

func TestRenderSDPAnswerForLayout_AudioOnlyOfferGetsNoVideoMLine(t *testing.T) {
	answer := renderSDPAnswerForLayout([]byte(goldenAnswerAVP), sipMediaLayoutFromOffer([]byte(goldenInviteAudioOnly)), nil)

	assertMediaLines(t, answer, "m=audio 4000 RTP/AVP 111 101")
}

func TestRenderSDPAnswerForLayout_VideoOfferWithoutGatewayVideoIsDeclined(t *testing.T) {
	answer := renderSDPAnswerForLayout([]byte(goldenAnswerAudioOnly), sipMediaLayoutFromOffer([]byte(goldenInviteAudioVideo)), nil)

	assertMediaLines(t, answer, "m=audio 4000 RTP/AVP 111 101", "m=video 0 RTP/AVP 99")
}

func TestRenderSDPOfferForLayout_KeepsGatewayVideoWhenPeerOfferedAudioOnly(t *testing.T) {
	offer := renderSDPOfferForLayout([]byte(goldenOfferAVP), sipMediaLayoutFromOffer([]byte(goldenInviteAudioOnly)), nil)

	if string(offer) != goldenOfferAVP {
		t.Fatalf("a gateway offer must keep its own audio and video\n got: %q\nwant: %q", offer, goldenOfferAVP)
	}
}

func TestHandleINVITE_ReinviteWithSecondVideoAnswersThreeMLinesAndKeepsCamera(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-second-video")
	sess.SetAsteriskEndpoints(
		&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 20000},
		&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 20002},
	)
	renegotiations := &midCallRenegotiationNotifierStub{}
	srv.midCallNotifier = renegotiations
	tx := &midCallServerTxStub{}

	srv.handleINVITE(newMidCallInviteRequest("call-second-video", offerWithSecondVideo), tx)

	assertResponseCodes(t, tx, []int{100, 200})
	assertMediaLines(t, tx.responses[1].Body(),
		"m=audio 4000 RTP/AVP 111 101",
		"m=video 4002 RTP/AVP 96",
		"m=video 0 RTP/AVPF 100",
	)
	if got := sess.AsteriskVideoAddr; got == nil || got.String() != "10.0.0.5:20002" {
		t.Fatalf("camera RTP destination = %v, want 10.0.0.5:20002", got)
	}
	_, audio, _ := sess.GetAudioRTPInfo()
	if audio == nil || audio.String() != "10.0.0.5:20000" {
		t.Fatalf("audio RTP destination = %v, want 10.0.0.5:20000", audio)
	}
	state := sess.GetMidCallMediaState()
	if !state.HasActiveVideo || state.VideoDirection != "sendrecv" {
		t.Fatalf("camera state must come from the first m=video, got %+v", state)
	}
	if renegotiations.calls != 0 {
		t.Fatalf("a declined second video must not ask the client to renegotiate, got %d calls", renegotiations.calls)
	}
	if _, pending := sess.GetPendingMidCallRenegotiation(); pending {
		t.Fatal("the renegotiation must be completed")
	}
	if got := len(sess.SIPMediaLayout()); got != 3 {
		t.Fatalf("recorded layout has %d slots, want 3", got)
	}
}

func TestHandleINVITE_ReinviteWithFewerMLinesShrinksAnswer(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-shrink")
	first := &midCallServerTxStub{}
	srv.handleINVITE(newMidCallInviteRequest("call-shrink", offerWithSecondVideo), first)
	assertResponseCodes(t, first, []int{100, 200})

	cameraOnly := offerWithSecondVideo[:strings.Index(offerWithSecondVideo, "m=video 20006")]
	second := &midCallServerTxStub{}
	srv.handleINVITE(newMidCallInviteRequest("call-shrink", cameraOnly), second)

	assertResponseCodes(t, second, []int{100, 200})
	assertMediaLines(t, second.responses[1].Body(),
		"m=audio 4000 RTP/AVP 111 101",
		"m=video 4002 RTP/AVP 96",
	)
	if got := len(sess.SIPMediaLayout()); got != 2 {
		t.Fatalf("recorded layout has %d slots, want 2", got)
	}
}

func TestHandleUPDATE_SecondVideoIsDeclinedInPlace(t *testing.T) {
	srv, sess := newMidCallTestServer(t, "call-update-second-video")
	sess.SetAsteriskEndpoints(
		&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 20000},
		&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 20002},
	)
	tx := &midCallServerTxStub{}

	srv.handleUPDATE(newMidCallUpdateRequest("call-update-second-video", offerWithSecondVideo), tx)

	assertResponseCodes(t, tx, []int{200})
	assertMediaLines(t, tx.responses[0].Body(),
		"m=audio 4000 RTP/AVP 111 101",
		"m=video 4002 RTP/AVP 96",
		"m=video 0 RTP/AVPF 100",
	)
	if got := sess.AsteriskVideoAddr; got == nil || got.String() != "10.0.0.5:20002" {
		t.Fatalf("camera RTP destination = %v, want 10.0.0.5:20002", got)
	}
}

func TestCreateHoldRequest_KeepsDeclinedMLine(t *testing.T) {
	srv := &Server{
		config:        config.SIPConfig{Domain: "203.0.113.20", Port: 5060},
		publicAddress: "203.0.113.10",
		sipPort:       5090,
	}
	sess := &session.Session{ID: "hold-declined"}
	sess.SetCallInfo("outbound", "sip:1001@203.0.113.20:5060", "sip:2002@203.0.113.20:5060", "call-hold-declined")
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:2002@203.0.113.20:5060>", "203.0.113.20", 5060, 7, nil)
	sess.SetRTPConnection(nil, 12000)
	sess.SetSIPMediaLayout(sipMediaLayoutFromOffer([]byte(offerWithSecondVideo)))

	hold, err := srv.createHoldRequest(sess, true)
	if err != nil {
		t.Fatalf("createHoldRequest: %v", err)
	}

	assertMediaLines(t, hold.Body(),
		"m=audio 12000 RTP/AVP 111 101",
		"m=video 12002 RTP/AVP 96",
		"m=video 0 RTP/AVPF 100",
	)
	if got := strings.Count(string(hold.Body()), "a=inactive"); got != 2 {
		t.Fatalf("hold must set inactive on audio and the camera only, got %d\nSDP:\n%s", got, hold.Body())
	}
	want := strings.ReplaceAll(goldenOfferAVP, "a=sendrecv", "a=inactive") + "m=video 0 RTP/AVPF 100\n"
	assertGoldenSDP(t, "hold with declined m-line", hold.Body(), want)
}

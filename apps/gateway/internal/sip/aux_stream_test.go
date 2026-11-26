package sip

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

// auxStreamRig is a SIP server whose peer is a function: every re-INVITE the
// gateway sends is handed to respond, which plays Asterisk.
type auxStreamRig struct {
	srv      *Server
	sess     *session.Session
	requests []*sip.Request
	respond  func(req *sip.Request) (*sip.Response, error)
}

func newAuxStreamRig(t *testing.T) *auxStreamRig {
	t.Helper()
	portMin, portMax := findFreeContiguousUDPBlock(t, 2)
	rig := &auxStreamRig{}
	rig.srv = &Server{
		config:        config.SIPConfig{Domain: "203.0.113.20", Port: 5060},
		rtpConfig:     config.RTPConfig{PortMin: portMin, PortMax: portMax, BufferSize: 2048},
		publicAddress: "203.0.113.10",
		sipPort:       5090,
	}
	rig.srv.glareBackoff = func(*session.Session) time.Duration { return 0 }
	rig.srv.inDialogInvite = func(req *sip.Request) (*sip.Response, error) {
		rig.requests = append(rig.requests, req)
		if rig.respond == nil {
			return nil, errors.New("no SIP peer configured")
		}
		return rig.respond(req)
	}

	sess := &session.Session{ID: "aux-stream", RTPBufferSize: 2048, State: session.StateActive}
	sess.SetCallInfo("outbound", "sip:1001@203.0.113.20:5060", "sip:2002@203.0.113.20:5060", "call-aux-stream")
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:2002@203.0.113.20:5060>", "203.0.113.20", 5060, 7, nil)
	sess.SetSIPAuthContext("trunk", "", 42, "203.0.113.20", "1001", "secret", 5060)
	sess.SetRTPConnection(nil, 12000)
	sess.SetAsteriskEndpoints(
		&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 20000},
		&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 20002},
	)
	t.Cleanup(func() { sess.CloseMediaTransports() })
	rig.sess = sess
	return rig
}

var sdpMediaPort = regexp.MustCompile(`(?m)^(m=\w+) \d+ `)

// answerMirroring returns the offer as an answer from 10.0.0.5 with the given
// port on each m-line, in order. Port 0 refuses that m-line.
func answerMirroring(req *sip.Request, ports ...int) (*sip.Response, error) {
	index := 0
	body := sdpMediaPort.ReplaceAllStringFunc(string(req.Body()), func(match string) string {
		port := 0
		if index < len(ports) {
			port = ports[index]
		}
		index++
		return fmt.Sprintf("%s %d ", strings.Fields(match)[0], port)
	})
	body = strings.ReplaceAll(body, "c=IN IP4 203.0.113.10", "c=IN IP4 10.0.0.5")
	body = strings.ReplaceAll(body, "a=sendonly", "a=recvonly")
	return sip.NewResponseFromRequest(req, 200, "OK", []byte(body)), nil
}

func refuseWith(code int, reason string) func(*sip.Request) (*sip.Response, error) {
	return func(req *sip.Request) (*sip.Response, error) {
		return sip.NewResponseFromRequest(req, code, reason, nil), nil
	}
}

func assertOutcome(t *testing.T, got ScreenStreamOutcome, status, reason string) {
	t.Helper()
	if got.Status != status || got.Reason != reason {
		t.Fatalf("outcome = %+v, want status=%s reason=%s", got, status, reason)
	}
}

func TestOpenScreenStreamAcceptedOpensSecondVideoMLine(t *testing.T) {
	rig := newAuxStreamRig(t)
	rig.respond = func(req *sip.Request) (*sip.Response, error) { return answerMirroring(req, 20000, 20002, 20006) }

	outcome := rig.srv.OpenScreenStream(rig.sess)

	assertOutcome(t, outcome, "ok", "")
	if len(rig.requests) != 1 || rig.requests[0].Method != sip.INVITE {
		t.Fatalf("expected one re-INVITE, got %d requests", len(rig.requests))
	}
	aux := rig.sess.AuxVideo()
	if aux == nil {
		t.Fatal("the auxiliary stream must be open")
	}
	auxPort, auxRTCPPort := aux.Ports()
	offer := rig.requests[0].Body()
	assertMediaLines(t, offer,
		"m=audio 12000 RTP/AVP 111 101",
		"m=video 12002 RTP/AVP 96",
		fmt.Sprintf("m=video %d RTP/AVP 96", auxPort),
	)
	auxSection := string(offer)[strings.LastIndex(string(offer), "m=video "):]
	for _, want := range []string{"a=rtpmap:96 H264/90000", "packetization-mode=1", "a=content:slides", "a=sendonly"} {
		if !strings.Contains(auxSection, want) {
			t.Fatalf("auxiliary section lacks %q:\n%s", want, auxSection)
		}
	}
	if strings.Contains(auxSection, "sprop-parameter-sets") || strings.Contains(auxSection, "a=sendrecv") {
		t.Fatalf("auxiliary section must not carry the camera's parameter sets or direction:\n%s", auxSection)
	}
	// The primary sections are exactly what a hold/resume offer carries.
	// (The session's SIP username appears in the o= line.)
	wantPrimary := strings.Replace(goldenOfferAVP, "o=- ", "o=1001 ", 1)
	if primary := string(offer)[:strings.LastIndex(string(offer), "m=video ")]; normalizeSDPOrigin([]byte(primary)) != wantPrimary {
		t.Fatalf("primary sections changed\n got: %q\nwant: %q", normalizeSDPOrigin([]byte(primary)), wantPrimary)
	}
	if auxRTCPPort != auxPort+1 {
		t.Fatalf("auxiliary RTCP port = %d, want %d", auxRTCPPort, auxPort+1)
	}

	if got := aux.RemoteRTPAddr(); got == nil || got.String() != "10.0.0.5:20006" {
		t.Fatalf("auxiliary RTP destination = %v, want 10.0.0.5:20006", got)
	}
	if got := rig.sess.AsteriskVideoAddr; got == nil || got.String() != "10.0.0.5:20002" {
		t.Fatalf("camera RTP destination = %v, want 10.0.0.5:20002", got)
	}
	if got := len(rig.sess.SIPMediaLayout()); got != 3 {
		t.Fatalf("layout has %d slots, want 3", got)
	}

	// Asking again while it is open is not another negotiation.
	assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "ok", "")
	if len(rig.requests) != 1 {
		t.Fatalf("an open stream must not be renegotiated, got %d requests", len(rig.requests))
	}
}

func TestOpenScreenStreamPortZeroIsRejectedAndCached(t *testing.T) {
	rig := newAuxStreamRig(t)
	rig.respond = func(req *sip.Request) (*sip.Response, error) { return answerMirroring(req, 20000, 20002, 0) }

	assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "rejected", "not_supported")

	if rig.sess.AuxVideo() != nil {
		t.Fatal("a refused stream must be closed")
	}
	if got := len(rig.sess.SIPMediaLayout()); got != 3 {
		t.Fatalf("the refused m-line stays in the dialog: layout has %d slots, want 3", got)
	}
	if got := rig.sess.AsteriskVideoAddr; got == nil || got.String() != "10.0.0.5:20002" {
		t.Fatalf("camera RTP destination = %v, want it untouched", got)
	}

	// Later SDP from the gateway keeps the m-line with port 0.
	hold, err := rig.srv.createHoldRequest(rig.sess, true)
	if err != nil {
		t.Fatal(err)
	}
	assertMediaLines(t, hold.Body(), "m=audio 12000 RTP/AVP 111 101", "m=video 12002 RTP/AVP 96", "m=video 0 RTP/AVP 96")

	// The account is remembered: no second re-INVITE.
	assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "rejected", "not_supported")
	if len(rig.requests) != 1 {
		t.Fatalf("cached rejection must not send a re-INVITE, got %d requests", len(rig.requests))
	}

	// After the cache expires the peer is asked again.
	rig.srv.auxRejects.mu.Lock()
	for key := range rig.srv.auxRejects.entries {
		rig.srv.auxRejects.entries[key] = time.Now().Add(-time.Second)
	}
	rig.srv.auxRejects.mu.Unlock()
	rig.respond = func(req *sip.Request) (*sip.Response, error) { return answerMirroring(req, 20000, 20002, 20006) }
	assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "ok", "")
	if len(rig.requests) != 2 {
		t.Fatalf("expected a new re-INVITE after expiry, got %d requests", len(rig.requests))
	}
	// The earlier refused m-line is reused; the dialog does not grow a fourth.
	if lines := sdpMediaLines(rig.requests[1].Body()); len(lines) != 3 {
		t.Fatalf("second offer m-lines = %q, want 3", lines)
	}
}

func TestOpenScreenStreamFinalErrorResponses(t *testing.T) {
	for _, tc := range []struct {
		code       int
		wantStatus string
		wantReason string
		wantCached bool
	}{
		{488, "rejected", "not_supported", true},
		{603, "rejected", "not_supported", true},
		{606, "rejected", "not_supported", true},
		{503, "rejected", "not_supported", false},
		{408, "rejected", "not_supported", false},
	} {
		t.Run(fmt.Sprint(tc.code), func(t *testing.T) {
			rig := newAuxStreamRig(t)
			rig.respond = refuseWith(tc.code, "Refused")

			assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), tc.wantStatus, tc.wantReason)

			if rig.sess.AuxVideo() != nil {
				t.Fatal("the stream must be closed")
			}
			if layout := rig.sess.SIPMediaLayout(); layout != nil {
				t.Fatalf("a failed re-INVITE leaves the dialog's SDP unchanged, got layout %+v", layout)
			}
			_ = rig.srv.OpenScreenStream(rig.sess)
			wantRequests := 2
			if tc.wantCached {
				wantRequests = 1
			}
			if len(rig.requests) != wantRequests {
				t.Fatalf("requests after a second attempt = %d, want %d", len(rig.requests), wantRequests)
			}
		})
	}
}

func TestOpenScreenStreamRetriesGlareOnce(t *testing.T) {
	rig := newAuxStreamRig(t)
	rig.respond = func(req *sip.Request) (*sip.Response, error) {
		if len(rig.requests) == 1 {
			return sip.NewResponseFromRequest(req, 491, "Request Pending", nil), nil
		}
		return answerMirroring(req, 20000, 20002, 20006)
	}

	assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "ok", "")

	if len(rig.requests) != 2 {
		t.Fatalf("requests = %d, want the first attempt and one retry", len(rig.requests))
	}
	first, second := rig.requests[0].CSeq(), rig.requests[1].CSeq()
	if first == nil || second == nil || second.SeqNo <= first.SeqNo {
		t.Fatalf("the retry must use a higher CSeq: %v then %v", first, second)
	}
}

func TestOpenScreenStreamGlareTwiceFails(t *testing.T) {
	rig := newAuxStreamRig(t)
	rig.respond = refuseWith(491, "Request Pending")

	assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "failed", "busy")

	if len(rig.requests) != 2 || rig.sess.AuxVideo() != nil {
		t.Fatalf("requests = %d aux open = %v, want 2 and closed", len(rig.requests), rig.sess.AuxVideo() != nil)
	}
	// Glare is not a refusal of the stream: nothing is cached.
	rig.respond = func(req *sip.Request) (*sip.Response, error) { return answerMirroring(req, 20000, 20002, 20006) }
	assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "ok", "")
}

func TestOpenScreenStreamTransportErrorFailsAndReleasesPorts(t *testing.T) {
	rig := newAuxStreamRig(t)
	rig.respond = func(*sip.Request) (*sip.Response, error) { return nil, errors.New("in-dialog INVITE timed out") }

	assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "failed", "sip_error")

	if rig.sess.AuxVideo() != nil {
		t.Fatal("the stream must be closed")
	}
	for i := 0; i < 2; i++ {
		_, rtpConn, rtcpConn, err := rig.srv.bindRTPPortPair()
		if err != nil {
			t.Fatalf("auxiliary ports were not released (slot %d): %v", i, err)
		}
		defer rtpConn.Close()
		defer rtcpConn.Close()
	}
}

func TestOpenScreenStreamPreconditions(t *testing.T) {
	t.Run("not active", func(t *testing.T) {
		rig := newAuxStreamRig(t)
		rig.sess.SetState(session.StateReconnecting)
		assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "failed", "not_active")
		if len(rig.requests) != 0 {
			t.Fatal("no re-INVITE may be sent")
		}
	})
	t.Run("held", func(t *testing.T) {
		rig := newAuxStreamRig(t)
		rig.sess.SetHeld(true)
		assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "failed", "held")
	})
	t.Run("no RTP port", func(t *testing.T) {
		rig := newAuxStreamRig(t)
		rig.srv.rtpConfig.PortMax = rig.srv.rtpConfig.PortMin
		assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "failed", "no_rtp_ports")
		if len(rig.requests) != 0 {
			t.Fatal("no re-INVITE may be sent")
		}
	})
	t.Run("audio-only call", func(t *testing.T) {
		rig := newAuxStreamRig(t)
		rig.sess.SetSIPOfferIncludeVideo(false)
		assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "failed", "video_call_required")
		if len(rig.requests) != 0 || rig.sess.AuxVideo() != nil {
			t.Fatal("no re-INVITE may be sent and the stream must be closed")
		}
	})
	t.Run("answer with fewer m-lines", func(t *testing.T) {
		rig := newAuxStreamRig(t)
		rig.respond = func(req *sip.Request) (*sip.Response, error) {
			body := string(req.Body())
			return sip.NewResponseFromRequest(req, 200, "OK", []byte(body[:strings.LastIndex(body, "m=video ")])), nil
		}
		assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "rejected", "not_supported")
		if rig.sess.AuxVideo() != nil {
			t.Fatal("the stream must be closed")
		}
	})
}

func TestCloseScreenStreamSendsPortZero(t *testing.T) {
	rig := newAuxStreamRig(t)
	rig.respond = func(req *sip.Request) (*sip.Response, error) { return answerMirroring(req, 20000, 20002, 20006) }
	assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "ok", "")

	if !rig.srv.CloseScreenStream(rig.sess, "client-remove") {
		t.Fatal("expected an open stream to be closed")
	}

	if rig.sess.AuxVideo() != nil {
		t.Fatal("the stream must be closed")
	}
	if len(rig.requests) != 2 {
		t.Fatalf("requests = %d, want the add and the remove", len(rig.requests))
	}
	assertMediaLines(t, rig.requests[1].Body(),
		"m=audio 12000 RTP/AVP 111 101", "m=video 12002 RTP/AVP 96", "m=video 0 RTP/AVP 96")

	if rig.srv.CloseScreenStream(rig.sess, "again") {
		t.Fatal("closing twice must report nothing was open")
	}
	if len(rig.requests) != 2 {
		t.Fatal("closing a closed stream must not send a re-INVITE")
	}

	// A new share on the same call reuses the m-line.
	assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "ok", "")
	if lines := sdpMediaLines(rig.requests[2].Body()); len(lines) != 3 || strings.HasPrefix(lines[2], "m=video 0 ") {
		t.Fatalf("re-add offer m-lines = %q, want the third reopened", lines)
	}
}

func TestHoldWhileSharingKeepsAuxiliarySection(t *testing.T) {
	rig := newAuxStreamRig(t)
	rig.respond = func(req *sip.Request) (*sip.Response, error) { return answerMirroring(req, 20000, 20002, 20006) }
	assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "ok", "")
	auxPort, _ := rig.sess.AuxVideo().Ports()

	hold, err := rig.srv.createHoldRequest(rig.sess, true)
	if err != nil {
		t.Fatal(err)
	}
	assertMediaLines(t, hold.Body(),
		"m=audio 12000 RTP/AVP 111 101", "m=video 12002 RTP/AVP 96", fmt.Sprintf("m=video %d RTP/AVP 96", auxPort))
	if got := strings.Count(string(hold.Body()), "a=inactive"); got != 3 {
		t.Fatalf("hold must set inactive on audio, camera and screen, got %d\n%s", got, hold.Body())
	}

	resume, err := rig.srv.createHoldRequest(rig.sess, false)
	if err != nil {
		t.Fatal(err)
	}
	body := string(resume.Body())
	if strings.Count(body, "a=sendrecv") != 2 || !strings.HasSuffix(body, "a=content:slides\na=sendonly\n") {
		t.Fatalf("resume must restore sendrecv on the primary streams and sendonly on the screen\n%s", body)
	}
}

type auxStateNotifierStub struct {
	midCallRenegotiationNotifierStub
	states []string
}

func (n *auxStateNotifierStub) NotifyAuxVideoState(sessionID string, active bool, reason, direction string) {
	n.states = append(n.states, fmt.Sprintf("%s:%v:%s", sessionID, active, reason))
}

func newSharingMidCallServer(t *testing.T, callID string) (*Server, *session.Session, *auxStateNotifierStub) {
	t.Helper()
	srv, sess := newMidCallTestServer(t, callID)
	srv.rtpConfig.PortMin, srv.rtpConfig.PortMax = findFreeContiguousUDPBlock(t, 1)
	notifier := &auxStateNotifierStub{}
	srv.midCallNotifier = notifier
	sess.SetAsteriskEndpoints(
		&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 20000},
		&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 20002},
	)
	aux, err := srv.openAuxVideoForSession(sess)
	if err != nil {
		t.Fatal(err)
	}
	aux.SetRemoteEndpoints(&net.UDPAddr{IP: net.ParseIP("10.0.0.5"), Port: 20006}, nil, true)
	return srv, sess, notifier
}

func TestInboundReinviteWhileSharingKeepsStreamAndFollowsPeer(t *testing.T) {
	srv, sess, notifier := newSharingMidCallServer(t, "call-sharing-reinvite")
	auxPort, _ := sess.AuxVideo().Ports()
	// Asterisk refreshes the session and moves the screen stream's port.
	offer := strings.Replace(offerWithSecondVideo, "m=video 20006 ", "m=video 20010 ", 1)
	tx := &midCallServerTxStub{}

	srv.handleINVITE(newMidCallInviteRequest("call-sharing-reinvite", offer), tx)

	assertResponseCodes(t, tx, []int{100, 200})
	answer := tx.responses[1].Body()
	// The screen m-line is answered with the offer's own transport and
	// payload type, which differ from the camera's here.
	assertMediaLines(t, answer,
		"m=audio 4000 RTP/AVP 111 101", "m=video 4002 RTP/AVP 96", fmt.Sprintf("m=video %d RTP/AVPF 100", auxPort))
	if !strings.Contains(string(answer), "a=rtpmap:100 H264/90000\na=fmtp:100 profile-level-id=42E01F;packetization-mode=1\na=rtcp-mux\n") {
		t.Fatalf("the screen m-line must mirror the offer's H.264 mapping\n%s", answer)
	}
	if !strings.HasSuffix(string(answer), "a=content:slides\na=sendonly\n") {
		t.Fatalf("the screen m-line must be answered as the sending side\n%s", answer)
	}
	aux := sess.AuxVideo()
	if aux == nil {
		t.Fatal("the stream must stay open")
	}
	if got := aux.RemoteRTPAddr(); got == nil || got.String() != "10.0.0.77:20010" {
		t.Fatalf("auxiliary RTP destination = %v, want the peer's new 10.0.0.77:20010", got)
	}
	if got := sess.AsteriskVideoAddr; got == nil || got.String() != "10.0.0.5:20002" {
		t.Fatalf("camera RTP destination = %v, want 10.0.0.5:20002", got)
	}
	if len(notifier.states) != 0 {
		t.Fatalf("no stream state change expected, got %v", notifier.states)
	}
}

func TestInboundReinviteClosingSecondVideoEndsStream(t *testing.T) {
	for name, offer := range map[string]string{
		"port 0":  strings.Replace(offerWithSecondVideo, "m=video 20006 ", "m=video 0 ", 1),
		"dropped": offerWithSecondVideo[:strings.Index(offerWithSecondVideo, "m=video 20006")],
	} {
		t.Run(name, func(t *testing.T) {
			srv, sess, notifier := newSharingMidCallServer(t, "call-sharing-closed")
			tx := &midCallServerTxStub{}

			srv.handleINVITE(newMidCallInviteRequest("call-sharing-closed", offer), tx)

			assertResponseCodes(t, tx, []int{100, 200})
			if sess.AuxVideo() != nil {
				t.Fatal("the stream must be closed")
			}
			if want := sess.ID + ":false:peer_closed"; len(notifier.states) != 1 || notifier.states[0] != want {
				t.Fatalf("stream state notifications = %v, want [%s]", notifier.states, want)
			}
			for _, line := range sdpMediaLines(tx.responses[1].Body())[2:] {
				if !strings.HasPrefix(line, "m=video 0 ") {
					t.Fatalf("closed m-line answered as %q", line)
				}
			}
		})
	}
}

func TestDescribeSDPMediaSections(t *testing.T) {
	sections := describeSDPMediaSections([]byte(offerWithSecondVideo))

	if len(sections) != 3 {
		t.Fatalf("sections = %d, want 3", len(sections))
	}
	camera, screen := sections[1], sections[2]
	if camera.kind != "video" || camera.port != 20002 || camera.ip != "10.0.0.5" || camera.rtcpMux ||
		camera.h264PT != 96 || camera.packetizationMode != 0 || camera.direction != "sendrecv" {
		t.Fatalf("camera section = %+v", camera)
	}
	if screen.kind != "video" || screen.port != 20006 || screen.ip != "10.0.0.77" || !screen.rtcpMux ||
		screen.h264PT != 100 || screen.packetizationMode != 1 || screen.direction != "sendonly" {
		t.Fatalf("screen section = %+v", screen)
	}
}

func TestBuildAuxSDPSectionDropsCameraParameterSetsAndRTCPPort(t *testing.T) {
	section := buildAuxSDPSection([]string{
		"m=video 4002 RTP/AVPF 99",
		"a=rtcp:4003",
		"a=rtpmap:99 H264/90000",
		"a=fmtp:99 profile-level-id=42E01F;packetization-mode=1;sprop-parameter-sets=Z0LgHw==,aM4G4g==",
		"a=rtcp-fb:* nack pli",
		"a=sendrecv",
	}, 5000, 5001, "recvonly")

	want := []string{
		"m=video 5000 RTP/AVPF 99",
		"a=rtcp:5001",
		"a=rtpmap:99 H264/90000",
		"a=fmtp:99 profile-level-id=42E01F;packetization-mode=1",
		"a=rtcp-fb:* nack pli",
		"a=content:slides",
		"a=recvonly",
	}
	if strings.Join(section, "|") != strings.Join(want, "|") {
		t.Fatalf("section = %q\nwant %q", section, want)
	}
	if buildAuxSDPSection(nil, 5000, 5001, "sendonly") != nil {
		t.Fatal("no primary video section means no auxiliary section")
	}
}

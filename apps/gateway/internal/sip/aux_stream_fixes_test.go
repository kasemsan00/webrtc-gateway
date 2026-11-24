package sip

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/pion/rtcp"

	"webrtc-sip-gateway/internal/config"
	"webrtc-sip-gateway/internal/session"
)

func TestInboundScreenShareSucceedsWithoutManualMidCallSeedAndWithMidCallDisabled(t *testing.T) {
	cfg := &config.Config{
		SIP: config.SIPConfig{MidCallRenegotiationEnable: false, Domain: "203.0.113.20", Port: 5060},
		RTP: config.RTPConfig{BufferSize: 2048},
	}
	cfg.RTP.PortMin, cfg.RTP.PortMax = findFreeContiguousUDPBlock(t, 3)
	mgr := session.NewManager(cfg)
	notifier := &auxPeerNotifier{canReceive: map[string]bool{}}
	srv := &Server{
		config:          cfg.SIP,
		rtpConfig:       cfg.RTP,
		sessionMgr:      mgr,
		publicAddress:   "203.0.113.10",
		sipPort:         5090,
		midCallNotifier: notifier,
	}

	sess, err := mgr.CreateSession(config.TURNConfig{})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() { mgr.DeleteSession(sess.ID) })

	// Simulate acceptCall seeding initial SIP media state WITHOUT manual ApplyMidCallSDPValidation.
	initialOffer := []byte("v=0\r\no=- 100 100 IN IP4 10.0.0.9\r\ns=-\r\nc=IN IP4 10.0.0.9\r\nt=0 0\r\n" +
		"m=audio 20000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\na=sendrecv\r\n" +
		"m=video 20002 RTP/AVP 96\r\na=rtpmap:96 H264/90000\r\na=fmtp:96 profile-level-id=42E01F;packetization-mode=1\r\na=sendrecv\r\n")
	sess.SetCallInfo("inbound", "sip:1001@203.0.113.20", "sip:agent7@203.0.113.20", "call-real-seed")
	sess.SetState(session.StateActive)
	sess.SetRTPConnection(nil, 12000)
	ans := srv.createSDPAnswerForInvite(12000, sess, initialOffer)
	sess.RememberSIPAnswer(ans)
	sess.SeedInitialSIPMediaState(string(initialOffer))
	ans, _ = srv.answerSDPForInboundOffer(sess, ans, initialOffer, false)
	sess.RememberLastWireSIPAnswer(ans)

	// Non-multiVideo client stays rejected with 501 when MidCallRenegotiationEnable=false.
	tx501 := &midCallServerTxStub{}
	srv.handleINVITE(newMidCallInviteRequest("call-real-seed", offerWithSecondVideo), tx501)
	assertResponseCodes(t, tx501, []int{100, 501})

	// MultiVideo client receives auxiliary stream cleanly with MidCallRenegotiationEnable=false.
	notifier.canReceive[sess.ID] = true
	tx200 := &midCallServerTxStub{}
	srv.handleINVITE(newMidCallInviteRequest("call-real-seed", offerWithSecondVideo), tx200)
	assertResponseCodes(t, tx200, []int{100, 200})

	aux := sess.AuxVideo()
	if aux == nil || aux.SIPDirection() != "recvonly" {
		t.Fatalf("expected receiving auxiliary stream open, got %v", aux)
	}
	auxPort, _ := aux.Ports()
	if lines := sdpMediaLines(tx200.responses[1].Body()); len(lines) != 3 || !strings.HasPrefix(lines[2], "m=video ") || strings.HasPrefix(lines[2], "m=video 0 ") {
		t.Fatalf("expected active 2nd m=video on port %d, got %q", auxPort, lines)
	}
	if notifier.calls != 0 {
		t.Fatalf("unexpected primary video_added renegotiation emitted: calls=%d", notifier.calls)
	}
	offered, _ := notifier.snapshot()
	if len(offered) != 1 || offered[0] != sess.ID {
		t.Fatalf("expected NotifyAuxVideoOffered for %s, got %v", sess.ID, offered)
	}
}

func TestOpenAndCloseScreenStreamAuthenticate401And407(t *testing.T) {
	t.Run("401 challenge retry and close", func(t *testing.T) {
		rig := newAuxStreamRig(t)
		rig.sess.SetSIPAuthContext("public", "1001@203.0.113.20:5060", 0, "203.0.113.20", "1001", "secret", 5060)
		var authHeaders []string
		rig.respond = func(req *sip.Request) (*sip.Response, error) {
			if len(rig.requests) == 1 {
				res := sip.NewResponseFromRequest(req, 401, "Unauthorized", nil)
				res.AppendHeader(sip.NewHeader("WWW-Authenticate", `Digest realm="asterisk", nonce="abc123", algorithm=md5, qop="auth"`))
				return res, nil
			}
			if h := req.GetHeader("Authorization"); h != nil {
				authHeaders = append(authHeaders, h.Value())
			}
			return answerMirroring(req, 20000, 20002, 20006)
		}

		assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "ok", "")
		if len(rig.requests) != 2 || len(authHeaders) != 1 {
			t.Fatalf("requests=%d authHeaders=%d, want 2 and 1", len(rig.requests), len(authHeaders))
		}
		if rig.requests[1].CSeq().SeqNo <= rig.requests[0].CSeq().SeqNo {
			t.Fatalf("retry CSeq %d must exceed initial %d", rig.requests[1].CSeq().SeqNo, rig.requests[0].CSeq().SeqNo)
		}
		if via := rig.requests[1].Via(); via == nil || via.Host != "203.0.113.10" || via.Port != 5090 {
			t.Fatalf("retry Via lost gateway host/port: %+v", via)
		}

		// Close also authenticates across a 401 challenge.
		rig.respond = func(req *sip.Request) (*sip.Response, error) {
			if h := req.GetHeader("Authorization"); h == nil {
				res := sip.NewResponseFromRequest(req, 401, "Unauthorized", nil)
				res.AppendHeader(sip.NewHeader("WWW-Authenticate", `Digest realm="asterisk", nonce="def456", algorithm=md5, qop="auth"`))
				return res, nil
			}
			return answerMirroring(req, 20000, 20002, 0)
		}
		if !rig.srv.CloseScreenStream(rig.sess, "done") {
			t.Fatal("CloseScreenStream should return true")
		}
		if len(rig.requests) != 4 {
			t.Fatalf("total requests = %d, want 4", len(rig.requests))
		}
	})

	t.Run("407 proxy auth and bad credentials not cached as not_supported", func(t *testing.T) {
		rig := newAuxStreamRig(t)
		rig.sess.SetSIPAuthContext("public", "1001@203.0.113.20:5060", 0, "203.0.113.20", "1001", "secret", 5060)
		rig.respond = func(req *sip.Request) (*sip.Response, error) {
			if h := req.GetHeader("Proxy-Authorization"); h == nil {
				res := sip.NewResponseFromRequest(req, 407, "Proxy Authentication Required", nil)
				res.AppendHeader(sip.NewHeader("Proxy-Authenticate", `Digest realm="proxy", nonce="p789", algorithm=MD5, qop="auth"`))
				return res, nil
			}
			// Reject second attempt too (bad password) -> must be failed/sip_error and uncached.
			return sip.NewResponseFromRequest(req, 407, "Proxy Authentication Required", nil), nil
		}

		assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "failed", "sip_error")
		if rig.srv.auxRejects.rejected(auxRejectKey(rig.sess), time.Now()) {
			t.Fatal("407 auth failure must not cache not_supported rejection")
		}
	})

	t.Run("SetHold authenticates across 401", func(t *testing.T) {
		rig := newAuxStreamRig(t)
		rig.sess.SetSIPAuthContext("public", "1001@203.0.113.20:5060", 0, "203.0.113.20", "1001", "secret", 5060)
		rig.respond = func(req *sip.Request) (*sip.Response, error) {
			if h := req.GetHeader("Authorization"); h == nil {
				res := sip.NewResponseFromRequest(req, 401, "Unauthorized", nil)
				res.AppendHeader(sip.NewHeader("WWW-Authenticate", `Digest realm="asterisk", nonce="hold1", algorithm=md5, qop="auth"`))
				return res, nil
			}
			return answerMirroring(req, 20000, 20002)
		}
		if err := rig.srv.SetHold(rig.sess, true); err != nil {
			t.Fatalf("SetHold failed: %v", err)
		}
		if !rig.sess.IsHeld() {
			t.Fatal("expected session held=true after 401 retry")
		}
	})
}

func TestMidCallSDPOriginVersionAndNegotiatedAVPF(t *testing.T) {
	cfg := &config.Config{
		SIP: config.SIPConfig{MidCallRenegotiationEnable: true, Domain: "203.0.113.20", Port: 5060},
		RTP: config.RTPConfig{BufferSize: 2048},
	}
	cfg.RTP.PortMin, cfg.RTP.PortMax = findFreeContiguousUDPBlock(t, 2)
	srv := &Server{
		config:        cfg.SIP,
		rtpConfig:     cfg.RTP,
		publicAddress: "203.0.113.10",
		sipPort:       5090,
	}

	sess := &session.Session{ID: "avpf-origin", RTPBufferSize: 2048, State: session.StateActive}
	t.Cleanup(func() { sess.CloseMediaTransports() })
	sess.SetCallInfo("inbound", "sip:1001@203.0.113.20", "sip:2002@203.0.113.20", "call-avpf-origin")
	sess.SetSIPDialogState("local-tag", "remote-tag", "<sip:2002@203.0.113.20:5060>", "203.0.113.20", 5060, 1, nil)
	sess.SetRTPConnection(nil, 12000)

	// Inbound INVITE negotiated RTP/AVPF + rtcp-mux even with AudioUseAVPF/VideoUseAVPF false.
	inboundAVPF := []byte("v=0\r\no=- 55 1 IN IP4 10.0.0.5\r\ns=-\r\nc=IN IP4 10.0.0.5\r\nt=0 0\r\n" +
		"m=audio 20000 RTP/AVPF 111\r\na=rtpmap:111 opus/48000/2\r\na=rtcp-mux\r\na=sendrecv\r\n" +
		"m=video 20002 RTP/AVPF 96\r\na=rtpmap:96 H264/90000\r\na=fmtp:96 profile-level-id=42E01F;packetization-mode=1\r\na=rtcp-mux\r\na=sendrecv\r\n")
	ans1 := srv.createSDPAnswerForInvite(12000, sess, inboundAVPF)
	sess.RememberSIPAnswer(ans1)
	ans1, _ = srv.answerSDPForInboundOffer(sess, ans1, inboundAVPF, false)
	ans1, _ = finalizeInboundAnswerSDP(sess, ans1)
	id1, ver1, ok1 := parseSDPOrigin(ans1)
	if !ok1 {
		t.Fatal("failed to parse initial o=")
	}

	// Identical refresh answer keeps the exact o= version.
	ansRefresh, changed := finalizeInboundAnswerSDP(sess, ans1)
	_, verRefresh, _ := parseSDPOrigin(ansRefresh)
	if changed || verRefresh != ver1 {
		t.Fatalf("unchanged answer changed=%v ver=%d, want false and %d", changed, verRefresh, ver1)
	}

	// Outgoing mid-call offer preserves RTP/AVPF, rtcp-mux, rtcp-fb and increments o= version with same sess-id.
	aux, err := srv.openAuxVideoForSession(sess)
	if err != nil {
		t.Fatalf("open aux: %v", err)
	}
	aux.SetSIPDirection("sendonly")
	offer, err := srv.buildMidCallOffer(sess, false)
	if err != nil {
		t.Fatalf("buildMidCallOffer: %v", err)
	}
	offerID, offerVer, okOffer := parseSDPOrigin(offer.body)
	if !okOffer || offerID != id1 || offerVer != ver1+1 {
		t.Fatalf("offer o=(%d, %d), want (%d, %d)", offerID, offerVer, id1, ver1+1)
	}
	offerStr := string(offer.body)
	if lines := sdpMediaLines(offer.body); len(lines) != 3 ||
		!strings.Contains(lines[0], "RTP/AVPF") ||
		!strings.Contains(lines[1], "RTP/AVPF") ||
		!strings.Contains(lines[2], "RTP/AVPF") {
		t.Fatalf("expected all 3 m-lines to keep RTP/AVPF, got %q\n%s", lines, offerStr)
	}
	if strings.Count(offerStr, "a=rtcp-fb:* nack pli") != 2 {
		t.Fatalf("expected rtcp-fb on primary and aux video:\n%s", offerStr)
	}
}

func TestDescribeSDPMediaSectionsMatchesH264FmtpByPayloadType(t *testing.T) {
	// fmtp for VP8 (97) has packetization-mode=1, while H264 (96) appears after its own fmtp without mode=1.
	sdp := []byte("v=0\r\nm=video 20006 RTP/AVP 97 96\r\n" +
		"a=fmtp:97 packetization-mode=1\r\n" +
		"a=fmtp:96 profile-level-id=42E01F\r\n" +
		"a=rtpmap:97 VP8/90000\r\n" +
		"a=rtpmap:96 H264/90000\r\n")
	sections := describeSDPMediaSections(sdp)
	if len(sections) != 1 || sections[0].h264PT != 96 || sections[0].packetizationMode != 0 {
		t.Fatalf("unexpected section parse: %+v", sections)
	}

	// fmtp before rtpmap for H264 PT 102 with packetization-mode=1.
	sdpBefore := []byte("v=0\r\nm=video 20006 RTP/AVP 102\r\n" +
		"a=fmtp:102 profile-level-id=42E01F;packetization-mode=1\r\n" +
		"a=rtpmap:102 H264/90000\r\n")
	sections2 := describeSDPMediaSections(sdpBefore)
	if len(sections2) != 1 || sections2[0].h264PT != 102 || sections2[0].packetizationMode != 1 {
		t.Fatalf("unexpected reverse-order section parse: %+v", sections2)
	}
}

func TestConfirmScreenStreamForcesImmediateBrowserPLI(t *testing.T) {
	rig := newAuxStreamRig(t)
	rig.respond = func(req *sip.Request) (*sip.Response, error) { return answerMirroring(req, 20000, 20002, 20006) }
	assertOutcome(t, rig.srv.OpenScreenStream(rig.sess), "ok", "")

	var plis int
	var mu sync.Mutex
	rig.sess.AuxVideo().BindUplinkTrack(0xCAFE, func([]rtcp.Packet) error {
		mu.Lock()
		plis++
		mu.Unlock()
		return nil
	})
	// Simulate "uplink-wait-idr" PLI fired immediately before peer confirmation.
	if !rig.sess.AuxVideo().RequestKeyframeFromBrowser("uplink-wait-idr") {
		t.Fatal("expected initial PLI to succeed")
	}
	// Peer confirms < 1s later: must still force a browser PLI.
	rig.srv.ConfirmScreenStream(rig.sess, true)
	mu.Lock()
	defer mu.Unlock()
	if plis != 2 {
		t.Fatalf("browser PLI count = %d, want 2 (unthrottled peer-ready)", plis)
	}
}

func TestReportScreenStreamToPeerUsesSwitchedAgentOnOutboundQueueCall(t *testing.T) {
	sess := &session.Session{ID: "queue-screen-route"}
	sess.SetCallInfo("outbound", "sip:client1@pbx.local", "sip:9999@pbx.local", "call-q")
	sess.SetSIPAuthContext("public", "client1@pbx.local:5060", 0, "pbx.local", "client1", "pw", 5060)
	sess.SetMessageRemoteUser("agent2001")

	dest, from, err := sessionScreenControlParties(sess)
	if err != nil {
		t.Fatalf("sessionScreenControlParties: %v", err)
	}
	if dest != "agent2001" || from != "client1" {
		t.Fatalf("dest=%q from=%q, want agent2001 and client1", dest, from)
	}
	// Also keep net imported use clean if we verify endpoint doesn't affect anything.
	_ = net.IPv4zero
}

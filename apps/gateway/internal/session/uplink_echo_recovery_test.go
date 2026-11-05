package session

import (
	"bytes"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"

	"webrtc-sip-gateway/internal/config"
)

// fuaIDR splits one IDR NAL into FU-A payloads of chunk bytes.
func fuaIDR(nal []byte, chunk int) [][]byte {
	hdr := nal[0]
	body := nal[1:]
	var out [][]byte
	for off := 0; off < len(body); off += chunk {
		end := off + chunk
		if end > len(body) {
			end = len(body)
		}
		fh := hdr & 0x1f
		if off == 0 {
			fh |= 0x80
		}
		if end == len(body) {
			fh |= 0x40
		}
		p := []byte{hdr&0xe0 | 28, fh}
		out = append(out, append(p, body[off:end]...))
	}
	return out
}

func countWireIDRs(got []rtp.Packet) (idr, sps, pps int) {
	for _, p := range got {
		switch {
		case uplinkHeldKeyframe(p.Payload, false):
			idr++
		case p.Payload[0]&0x1f == 7:
			sps++
		case p.Payload[0]&0x1f == 8:
			pps++
		}
	}
	return
}

// An FU-A IDR whose start fragment was lost must not reach SIP once the
// uplink gate is open: it has no SPS/PPS, cannot be decoded, and used to be
// recorded as a forwarded keyframe that silenced every later IDR request.
func TestUplinkOrphanFUAIDRTailIsDropped(t *testing.T) {
	frags := fuaIDR(paddedNAL(0x65, 3000), 1000)
	input := [][]byte{
		// Start fragment of the IDR at 90000 is missing.
		mode0SlicePacket(2, 90000, false, frags[1]),
		mode0SlicePacket(3, 90000, true, frags[2]),
		mode0SlicePacket(4, 93000, true, paddedNAL(0x41, 100)),
	}
	got, sess := forwardUplinkVideoForTest(t, input, nil, func(s *Session) {
		s.SetSIPVideoPacketizationMode(1)
		// Gate already open from an earlier IDR of this call.
		s.LastUplinkKeyframe = time.Now().Add(-5 * time.Second)
	})
	if idr, _, _ := countWireIDRs(got); idr != 0 {
		t.Fatalf("orphan IDR fragments reached SIP: %d", idr)
	}
	if sess.uplinkParameterObservation.idrs != 0 || sess.uplinkParameterObservation.missing != 0 {
		t.Fatalf("orphan tail was observed as an IDR: %+v", sess.uplinkParameterObservation)
	}
	if time.Since(sess.LastUplinkKeyframe) < 4*time.Second {
		t.Fatal("orphan tail refreshed LastUplinkKeyframe")
	}
	if sess.LastBrowserPLIReason() != "idr_without_start" {
		t.Fatalf("browser PLI reason = %q, want idr_without_start", sess.LastBrowserPLIReason())
	}
}

// An IDR start that arrives before the SIP video address is set must not
// open the uplink gate or use up the SPS/PPS prefix of its access unit. The
// tail and the following P-frame stay off the wire; the next IDR goes out
// with SPS+PPS in front of it.
func TestUplinkIDRStartBeforeSIPAddressDoesNotOpenGate(t *testing.T) {
	sps := []byte{0x67, 0x42, 0x00, 0x1f, 0xab}
	pps := []byte{0x68, 0xce, 0x06, 0xe2}
	first := fuaIDR(paddedNAL(0x65, 3000), 1000)
	second := fuaIDR(paddedNAL(0x65, 2000), 1000)
	input := [][]byte{
		mode0SlicePacket(1, 90000, false, first[0]),
		mode0SlicePacket(2, 90000, false, first[1]),
		mode0SlicePacket(3, 90000, true, first[2]),
		mode0SlicePacket(4, 93000, true, paddedNAL(0x41, 100)),
		mode0SlicePacket(5, 96000, false, second[0]),
		mode0SlicePacket(6, 96000, true, second[1]),
	}
	delays := make([]time.Duration, len(input))
	delays[1] = 150 * time.Millisecond
	got, sess := forwardUplinkVideoForTest(t, input, delays, func(s *Session) {
		s.SetSIPVideoPacketizationMode(1)
		addr := s.AsteriskVideoAddr
		s.AsteriskVideoAddr = nil
		go func() {
			time.Sleep(50 * time.Millisecond)
			s.mu.Lock()
			s.AsteriskVideoAddr = addr
			s.mu.Unlock()
		}()
	})
	for _, p := range got {
		if p.Timestamp != got[0].Timestamp {
			t.Fatalf("packet from the unwritten IDR or its P-frame reached SIP: %+v", p.Header)
		}
	}
	if len(got) != 4 {
		t.Fatalf("packets=%d, want SPS+PPS+two IDR fragments of the second IDR", len(got))
	}
	if !bytes.Equal(got[0].Payload, sps) || !bytes.Equal(got[1].Payload, pps) {
		t.Fatalf("second IDR prefix = %x / %x", got[0].Payload, got[1].Payload)
	}
	if sess.uplinkParameterObservation.missing != 0 {
		t.Fatalf("observation: %+v", sess.uplinkParameterObservation)
	}
}

func TestDecideReturnRecoveryUplinkKick(t *testing.T) {
	now := time.Unix(1_900_000_000, 0)
	base := returnRecoveryUplinkKickInput{RemoteVideoSSRC: 42, Now: now}
	cases := []struct {
		name string
		mut  func(*returnRecoveryUplinkKickInput)
		want string
	}{
		{"send", func(*returnRecoveryUplinkKickInput) {}, ""},
		{"return keyframe answered", func(in *returnRecoveryUplinkKickInput) {
			in.ReturnKeyframeAt = now.Add(-time.Second)
			in.Unanswered = 1999 * time.Millisecond
		}, "return-keyframe-answered"},
		// 1mtLasLjXLNu: one return IDR, client still asking 2s+ later.
		{"return keyframe not decoded", func(in *returnRecoveryUplinkKickInput) {
			in.ReturnKeyframeAt = now.Add(-time.Minute)
			in.Unanswered = 2 * time.Second
		}, ""},
		{"no sip video", func(in *returnRecoveryUplinkKickInput) { in.RemoteVideoSSRC = 0 }, "no-sip-video"},
		{"max", func(in *returnRecoveryUplinkKickInput) { in.Kicks = returnRecoveryUplinkKickMax }, "max"},
		{"in flight", func(in *returnRecoveryUplinkKickInput) { in.UplinkIDRFlight = true }, "idr-in-flight"},
		{"rate limit", func(in *returnRecoveryUplinkKickInput) { in.LastKickAt = now.Add(-1999 * time.Millisecond) }, "rate-limit"},
		{"rate limit elapsed", func(in *returnRecoveryUplinkKickInput) { in.LastKickAt = now.Add(-2 * time.Second) }, ""},
		{"uplink idr fresh", func(in *returnRecoveryUplinkKickInput) { in.LastUplinkIDR = now.Add(-time.Second) }, "idr-fresh"},
		{"uplink idr old", func(in *returnRecoveryUplinkKickInput) { in.LastUplinkIDR = now.Add(-63 * time.Second) }, ""},
	}
	for _, tc := range cases {
		in := base
		tc.mut(&in)
		if got := decideReturnRecoveryUplinkKick(in); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Android → 9999: the phone keeps sending PLI while the echo return has no
// keyframe. The gateway asks the phone's encoder for an IDR, bounded by the
// interval and the per-call cap, and stops once a return keyframe arrives.
func TestReturnRecoveryKicksUplinkUntilReturnKeyframe(t *testing.T) {
	sess := &Session{ID: "echo-return", RemoteVideoSSRC: 99}
	sess.LastUplinkKeyframe = time.Now().Add(-10 * time.Second)
	now := time.Now()
	if sess.maybeKickUplinkForReturnRecovery("switch", now) {
		t.Fatal("switch trigger must not use the return-recovery kick")
	}
	if !sess.maybeKickUplinkForReturnRecovery("browser-pli", now) {
		t.Fatal("first browser PLI without a return keyframe did not kick the uplink")
	}
	if sess.maybeKickUplinkForReturnRecovery("browser-pli", now.Add(200*time.Millisecond)) {
		t.Fatal("second kick inside the interval")
	}
	kicks := 1
	for i := 1; i < 2*returnRecoveryUplinkKickMax; i++ {
		if sess.maybeKickUplinkForReturnRecovery("browser-pli", now.Add(time.Duration(i)*returnRecoveryUplinkKickInterval)) {
			kicks++
		}
	}
	if kicks != returnRecoveryUplinkKickMax {
		t.Fatalf("kicks=%d, want cap %d", kicks, returnRecoveryUplinkKickMax)
	}

	sess = &Session{ID: "echo-return-ok", RemoteVideoSSRC: 99}
	sess.LastKeyframe = time.Now()
	if sess.maybeKickUplinkForReturnRecovery("browser-pli", time.Now()) {
		t.Fatal("kicked after a return keyframe was seen")
	}
}

func TestSIPKeyframeINFORecoveryFloor(t *testing.T) {
	sess := &Session{ID: "info-floor"}
	t0 := time.Unix(1_900_000_000, 0)
	if ok, recovering := sess.allowSIPKeyframeINFO(t0); !ok || !recovering {
		t.Fatalf("first INFO: ok=%v recovering=%v", ok, recovering)
	}
	if ok, _ := sess.allowSIPKeyframeINFO(t0.Add(499 * time.Millisecond)); ok {
		t.Fatal("INFO inside the 500ms recovery floor")
	}
	if ok, recovering := sess.allowSIPKeyframeINFO(t0.Add(500 * time.Millisecond)); !ok || !recovering {
		t.Fatalf("unanswered INFO retry at 500ms: ok=%v recovering=%v", ok, recovering)
	}
	// A return keyframe answers the INFO and the client stopped asking:
	// back to the 2s floor.
	sess.LastKeyframe = t0.Add(700 * time.Millisecond)
	if ok, recovering := sess.allowSIPKeyframeINFO(t0.Add(1500 * time.Millisecond)); ok || recovering {
		t.Fatalf("answered INFO at +1s: ok=%v recovering=%v, want rate-limited at 2s floor", ok, recovering)
	}
	if ok, _ := sess.allowSIPKeyframeINFO(t0.Add(2500 * time.Millisecond)); !ok {
		t.Fatal("answered INFO after 2s floor")
	}
	// 1mtLasLjXLNu: the return keyframe arrived but the client keeps asking.
	sess.LastKeyframe = t0.Add(2600 * time.Millisecond)
	sess.noteClientKeyframeRequest("browser-pli", t0.Add(2700*time.Millisecond))
	sess.noteClientKeyframeRequest("browser-pli", t0.Add(4300*time.Millisecond))
	if ok, recovering := sess.allowSIPKeyframeINFO(t0.Add(4300 * time.Millisecond)); !ok || !recovering {
		t.Fatalf("client still waiting 1.6s after keyframe: ok=%v recovering=%v, want 500ms floor", ok, recovering)
	}
}

func TestReturnRecoveryActiveThresholds(t *testing.T) {
	now := time.Unix(1_900_000_000, 0)
	sess := &Session{ID: "rr-active"}
	if sess.returnRecoveryActiveLocked(now) {
		t.Fatal("active without any client request")
	}
	sess.LastKeyframe = now.Add(-30 * time.Second)
	sess.noteClientKeyframeRequest("switch", now)
	if !sess.lastClientKeyframeRequestAt.IsZero() {
		t.Fatal("switch is not a client decoder request")
	}
	sess.noteClientKeyframeRequest("browser-pli", now)
	if sess.returnRecoveryActiveLocked(now.Add(browserFIRStale - time.Millisecond)) {
		t.Fatal("active before browserFIRStale of unanswered asking")
	}
	sess.noteClientKeyframeRequest("browser-fir", now.Add(browserFIRStale))
	if !sess.returnRecoveryActiveLocked(now.Add(browserFIRStale)) {
		t.Fatal("not active after browserFIRStale of unanswered asking")
	}
	if !sess.BrowserNeedsReturnKeyframe(now.Add(browserFIRStale + returnRecoveryRequestWindow)) {
		t.Fatal("request window should still cover the last request")
	}
	if sess.BrowserNeedsReturnKeyframe(now.Add(browserFIRStale + returnRecoveryRequestWindow + time.Millisecond)) {
		t.Fatal("client stopped asking: recovery must end")
	}
	// A keyframe that arrives restarts the unanswered clock.
	sess.LastKeyframe = now.Add(browserFIRStale + 100*time.Millisecond)
	sess.noteClientKeyframeRequest("ws-request_keyframe", now.Add(browserFIRStale+200*time.Millisecond))
	if sess.returnRecoveryActiveLocked(now.Add(browserFIRStale + 200*time.Millisecond)) {
		t.Fatal("fresh return keyframe must reset the unanswered clock")
	}
}

// 1mtLasLjXLNu: one return IDR made LastKeyframe nonzero and the aadffd8
// return-recovery kick never fired again while the phone stayed black.
func TestReturnRecoveryKicksUplinkAfterUndecodedReturnKeyframe(t *testing.T) {
	now := time.Now()
	sess := &Session{ID: "echo-undecoded", RemoteVideoSSRC: 99}
	sess.LastKeyframe = now.Add(-30 * time.Second)
	sess.LastUplinkKeyframe = now.Add(-10 * time.Second)

	sess.noteClientKeyframeRequest("browser-pli", now)
	if sess.maybeKickUplinkForReturnRecovery("browser-pli", now) {
		t.Fatal("kicked before the client asked for returnRecoveryUplinkKickInterval")
	}
	at := now.Add(returnRecoveryUplinkKickInterval)
	sess.noteClientKeyframeRequest("browser-pli", at)
	if !sess.maybeKickUplinkForReturnRecovery("browser-pli", at) {
		t.Fatal("client still asking 2s after an undecoded return keyframe: expected uplink kick")
	}

	// Cap reached, then the client goes quiet: the next episode re-arms it.
	sess.returnRecoveryUplinkKicks = returnRecoveryUplinkKickMax
	at = at.Add(returnRecoveryUplinkKickInterval)
	sess.noteClientKeyframeRequest("browser-pli", at)
	if sess.maybeKickUplinkForReturnRecovery("browser-pli", at) {
		t.Fatal("kicked past the per-episode cap")
	}
	episode := at.Add(returnRecoveryEpisodeIdle)
	sess.noteClientKeyframeRequest("browser-pli", episode)
	if sess.returnRecoveryUplinkKicks != 0 {
		t.Fatalf("new episode kicks=%d, want 0", sess.returnRecoveryUplinkKicks)
	}
	at = episode.Add(returnRecoveryUplinkKickInterval)
	sess.noteClientKeyframeRequest("browser-pli", at)
	if !sess.maybeKickUplinkForReturnRecovery("browser-pli", at) {
		t.Fatal("new episode did not kick")
	}

	// A return keyframe that the client accepts stops the kicks.
	sess.LastKeyframe = at.Add(time.Second)
	at = at.Add(returnRecoveryUplinkKickInterval + 500*time.Millisecond)
	sess.noteClientKeyframeRequest("browser-pli", at)
	if sess.maybeKickUplinkForReturnRecovery("browser-pli", at) {
		t.Fatal("kicked 1.5s after a fresh return keyframe")
	}
}

func TestDecideKeyframeWatchdogDoesNotSkipWhileClientWaits(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	in := KeyframeWatchdogInput{
		Now: now, LastKeyframe: now.Add(-6 * time.Second), LastRTP: now.Add(-10 * time.Millisecond),
		Stale: 4 * time.Second, FIRStale: 8 * time.Second, Interval: 2 * time.Second, HealthyIDR: true,
	}
	if got := DecideKeyframeWatchdog(in); got.Reason != "rtp-flowing" {
		t.Fatalf("client not asking: want rtp-flowing skip, got %+v", got)
	}
	in.BrowserNeedsKeyframe = true
	if got := DecideKeyframeWatchdog(in); got.Action != KeyframeWatchdogPLI || got.Reason != "keyframe-stale" {
		t.Fatalf("client waiting: want PLI, got %+v", got)
	}
	in.LastKeyframe = now.Add(-50 * time.Second)
	if got := DecideKeyframeWatchdog(in); got.Action != KeyframeWatchdogFIR {
		t.Fatalf("client waiting 50s: want FIR, got %+v", got)
	}
	in.BrowserNeedsKeyframe = false
	in.LastKeyframe = time.Time{}
	if got := DecideKeyframeWatchdog(in); got.Action != KeyframeWatchdogPLI {
		t.Fatalf("no return keyframe ever: want PLI, got %+v", got)
	}
}

func TestDecideSIPKeyframeFeedbackModeInfoEscalatesForReturnRecovery(t *testing.T) {
	now := time.Unix(1_900_000_400, 0)
	base := SIPKeyframeFeedbackInput{
		Mode: config.SIPKeyframeRequestModeInfo, Kind: "fir", Now: now,
		Guard: 1500 * time.Millisecond, MinInterval: 2 * time.Second,
		PacketSeen: true, LastPacketMarker: true, ReturnRecovery: true,
	}
	got := DecideSIPKeyframeFeedback(base)
	if !got.SendRTCP || !got.SendINFO || !got.Escalated || got.Reason != "" {
		t.Fatalf("escalated: %+v", got)
	}
	in := base
	in.InFlight = true
	if got := DecideSIPKeyframeFeedback(in); got.SendRTCP || !got.SendINFO || got.Reason != "idr-in-flight" {
		t.Fatalf("escalated in-flight: %+v", got)
	}
	in = base
	in.CompleteAt = now.Add(-time.Second)
	if got := DecideSIPKeyframeFeedback(in); got.SendRTCP || got.Reason != "idr-guard" {
		t.Fatalf("escalated guard: %+v", got)
	}
	in = base
	in.LastRTCP = now.Add(-time.Second)
	if got := DecideSIPKeyframeFeedback(in); got.SendRTCP || !got.SendINFO || got.Reason != "rate-limit" {
		t.Fatalf("escalated rate-limit: %+v", got)
	}
	in = base
	in.LastPacketMarker = false
	if got := DecideSIPKeyframeFeedback(in); got.SendRTCP || !got.PendingRTCP || got.Reason != "mid-frame" {
		t.Fatalf("escalated mid-frame: %+v", got)
	}
	in = base
	in.ReturnRecovery = false
	if got := DecideSIPKeyframeFeedback(in); got.SendRTCP || got.Escalated || got.Reason != "mode-info" {
		t.Fatalf("no return recovery stays INFO-only: %+v", got)
	}
}

func TestSendSIPFeedbackModeInfoEscalatesWhileClientWaits(t *testing.T) {
	sess := newBurstTestSession("sip-fb-escalate")
	makeSIPVideoRecoveryReady(t, sess)
	sess.sipKeyframeRequestMode = config.SIPKeyframeRequestModeInfo
	sess.sipRTCPFBMinInterval = 2 * time.Second
	sess.sipRTCPFBGuard = 1500 * time.Millisecond
	var infos atomic.Int32
	sess.SetSIPKeyframeINFOSender(func(string) { infos.Add(1) })

	now := time.Now()
	sess.LastKeyframe = now.Add(-30 * time.Second)
	sess.noteClientKeyframeRequest("browser-pli", now.Add(-500*time.Millisecond))
	sess.SendPLIToAsteriskForced("browser-pli")
	if !sess.LastSipPLISent.IsZero() {
		t.Fatal("RTCP before the client asked for browserFIRStale")
	}

	// Client has been asking for 3s, last request just now.
	sess.mu.Lock()
	sess.clientKeyframeEpisodeAt = now.Add(-3 * time.Second)
	sess.lastClientKeyframeRequestAt = now
	sess.mu.Unlock()
	sess.SendPLIToAsteriskForced("browser-pli")
	if sess.LastSipPLISent.IsZero() {
		t.Fatal("mode info did not escalate to RTCP PLI while the client waits")
	}
	waitFor(t, time.Second, func() bool { return infos.Load() >= 1 })
	if infos.Load() < 1 {
		t.Fatal("escalation must still send SIP INFO")
	}
}

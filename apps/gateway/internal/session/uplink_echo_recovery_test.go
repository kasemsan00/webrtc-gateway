package session

import (
	"bytes"
	"testing"
	"time"

	"github.com/pion/rtp"
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
		{"return keyframe seen", func(in *returnRecoveryUplinkKickInput) { in.ReturnKeyframeAt = now.Add(-time.Minute) }, "return-keyframe-seen"},
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
	if ok, _ := sess.allowSIPKeyframeINFO(t0.Add(999 * time.Millisecond)); ok {
		t.Fatal("INFO inside the 1s recovery floor")
	}
	if ok, recovering := sess.allowSIPKeyframeINFO(t0.Add(time.Second)); !ok || !recovering {
		t.Fatalf("unanswered INFO retry at 1s: ok=%v recovering=%v", ok, recovering)
	}
	// A return keyframe answers the INFO: back to the 2s floor.
	sess.LastKeyframe = t0.Add(1500 * time.Millisecond)
	if ok, recovering := sess.allowSIPKeyframeINFO(t0.Add(2500 * time.Millisecond)); ok || recovering {
		t.Fatalf("answered INFO at +1.5s: ok=%v recovering=%v, want rate-limited at 2s floor", ok, recovering)
	}
	if ok, _ := sess.allowSIPKeyframeINFO(t0.Add(3 * time.Second)); !ok {
		t.Fatal("answered INFO after 2s floor")
	}
}

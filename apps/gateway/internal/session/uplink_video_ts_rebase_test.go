package session

import (
	"testing"
	"time"

	"github.com/pion/rtp"
)

func newTestRebaser() *uplinkVideoTSRebaser {
	return &uplinkVideoTSRebaser{base: DefaultSIPVideoTSBase}
}

func TestUplinkTSRebaseFirstPacketNear42e9MapsToBase(t *testing.T) {
	r := newTestRebaser()
	now := time.Unix(1_700_000_000, 0)
	if out, d := r.mapTS(4_200_000_000, 1, now); out != 5400000 || d == nil || !d.startedThisCall {
		t.Fatalf("first packet out=%d event=%+v, want base 5400000 and a start event", out, d)
	}
	if out, d := r.mapTS(4_200_003_000, 1, now.Add(33*time.Millisecond)); out != 5403000 || d != nil {
		t.Fatalf("next frame out=%d event=%+v, want 5403000", out, d)
	}
}

func TestUplinkTSRebasePreservesDeltasAcrossWrap(t *testing.T) {
	r := newTestRebaser()
	now := time.Unix(1_700_000_000, 0)
	in := uint32(0xFFFFF000) // 4096 ticks before wrap
	r.mapTS(in, 1, now)
	for i, delta := range []uint32{3000, 3000, 3000, 2999, 1} {
		in += delta // wraps on the second step
		now = now.Add(33 * time.Millisecond)
		want := DefaultSIPVideoTSBase + (in - 0xFFFFF000)
		if out, d := r.mapTS(in, 1, now); out != want || d != nil {
			t.Fatalf("step %d in=%d out=%d want %d event=%+v", i, in, out, want, d)
		}
	}
	if in > 0xFFFFF000 {
		t.Fatal("test did not wrap")
	}
}

func TestUplinkTSRebaseSameTimestampAndLatePackets(t *testing.T) {
	r := newTestRebaser()
	now := time.Unix(1_700_000_000, 0)
	r.mapTS(1000, 1, now)
	for i := 0; i < 3; i++ { // three slices of one picture
		if out, _ := r.mapTS(1000, 1, now); out != DefaultSIPVideoTSBase {
			t.Fatalf("slice %d out=%d, want the picture's base", i, out)
		}
	}
	r.mapTS(7000, 1, now.Add(66*time.Millisecond))
	// A late slice of the 1000 picture maps on the same offset and does not
	// move the timeline.
	if out, d := r.mapTS(1000, 1, now.Add(70*time.Millisecond)); out != DefaultSIPVideoTSBase || d != nil {
		t.Fatalf("late slice out=%d event=%+v", out, d)
	}
	if out, _ := r.mapTS(10000, 1, now.Add(100*time.Millisecond)); out != DefaultSIPVideoTSBase+9000 {
		t.Fatalf("after late slice out=%d, want base+9000", out)
	}
}

func TestUplinkTSRebaseNeverGoesBackward(t *testing.T) {
	r := newTestRebaser()
	now := time.Unix(1_700_000_000, 0)
	last, _ := r.mapTS(500_000, 1, now)
	steps := []struct {
		name   string
		in     uint32
		ssrc   uint32
		wall   time.Duration
		reason string
	}{
		{"frame", 503_000, 1, 33 * time.Millisecond, ""},
		{"ssrc change to an older ts", 100, 2, 40 * time.Millisecond, "ssrc-change"},
		{"frame on new ssrc", 3100, 2, 33 * time.Millisecond, ""},
		{"big backward jump (wraps below zero)", 1<<32 - 996_900, 2, 50 * time.Millisecond, "ts-backward"},
		{"big forward jump", 19_003_100, 2, 20 * time.Second, "ts-jump"},
		{"frame after jump", 19_006_100, 2, 33 * time.Millisecond, ""},
	}
	for _, st := range steps {
		now = now.Add(st.wall)
		out, d := r.mapTS(st.in, st.ssrc, now)
		if int32(out-last) <= 0 {
			t.Fatalf("%s: out=%d after %d went backward or repeated", st.name, out, last)
		}
		switch {
		case st.reason == "" && d != nil:
			t.Fatalf("%s: unexpected discontinuity %+v", st.name, d)
		case st.reason != "" && (d == nil || d.reason != st.reason):
			t.Fatalf("%s: event=%+v, want reason %s", st.name, d, st.reason)
		case st.reason != "":
			// Wall-clock step, like the SIP->WebRTC timeline.
			assertWallClockTimestampStep(t, out-last, st.wall)
		default:
			if out-last != 3000 {
				t.Fatalf("%s: step %d, want the browser delta 3000", st.name, out-last)
			}
		}
		last = out
	}
}

// uplinkTSPackets builds browser video RTP packets on one SSRC.
func uplinkTSPacket(seq uint16, ts uint32, ssrc uint32, marker bool, payload []byte) []byte {
	raw, err := (&rtp.Packet{Header: rtp.Header{
		Version: 2, PayloadType: 104, SequenceNumber: seq, Timestamp: ts, SSRC: ssrc, Marker: marker,
	}, Payload: payload}).Marshal()
	if err != nil {
		panic(err)
	}
	return raw
}

func rebaseSetup(rebase bool, mode uint8, open bool) func(*Session) {
	return func(s *Session) {
		s.sipVideoTSRebase = rebase
		s.SetSIPVideoPacketizationMode(mode)
		if open {
			s.LastUplinkKeyframe = time.Now()
		}
	}
}

// Mode 0, uplink already open, IDR without in-band SPS/PPS: the gateway
// injects SPS/PPS, and they must share the IDR's rebased timestamp.
func TestUplinkTSRebaseInjectedParameterSetsShareIDRTimestamp(t *testing.T) {
	const first = uint32(4_200_000_000)
	const ssrc = 0x11223344
	packets := [][]byte{
		uplinkTSPacket(1, first, ssrc, false, paddedNAL(0x65, 300)),
		uplinkTSPacket(2, first, ssrc, true, paddedNAL(0x65, 300)),
		uplinkTSPacket(3, first+3000, ssrc, true, paddedNAL(0x41, 200)),
		uplinkTSPacket(4, first+6000, ssrc, true, paddedNAL(0x41, 200)),
	}
	got, _ := forwardUplinkVideoForTest(t, packets, nil, rebaseSetup(true, 0, true))
	if len(got) < 6 {
		t.Fatalf("forwarded %d packets, want injected SPS/PPS + 4", len(got))
	}
	if got[0].Timestamp != DefaultSIPVideoTSBase {
		t.Fatalf("first outbound ts=%d, want %d", got[0].Timestamp, DefaultSIPVideoTSBase)
	}
	sawSPS, sawPPS := false, false
	for _, p := range got {
		nal := p.Payload[0] & 0x1f
		switch nal {
		case 7, 8, 5:
			if p.Timestamp != DefaultSIPVideoTSBase {
				t.Fatalf("NAL %d ts=%d, want the IDR's %d", nal, p.Timestamp, DefaultSIPVideoTSBase)
			}
			sawSPS = sawSPS || nal == 7
			sawPPS = sawPPS || nal == 8
		case 1:
			if d := p.Timestamp - DefaultSIPVideoTSBase; d != 3000 && d != 6000 {
				t.Fatalf("P frame ts=%d, want base+3000/6000", p.Timestamp)
			}
		}
	}
	if !sawSPS || !sawPPS {
		t.Fatalf("no injected SPS/PPS (sps=%v pps=%v)", sawSPS, sawPPS)
	}
	assertUplinkRTPContinuity(t, got)
}

// STAP-A [SPS, PPS, IDR] is de-aggregated; every NAL keeps one rebased ts.
func TestUplinkTSRebaseSTAPANALsShareTimestamp(t *testing.T) {
	const first = uint32(3_000_000_000)
	const ssrc = 0x55667788
	stap := []byte{0x78}
	for _, nal := range [][]byte{{0x67, 0x42, 0x00, 0x1f, 0xab}, {0x68, 0xce, 0x06, 0xe2}, paddedNAL(0x65, 200)} {
		stap = append(stap, byte(len(nal)>>8), byte(len(nal)))
		stap = append(stap, nal...)
	}
	packets := [][]byte{
		uplinkTSPacket(1, first, ssrc, true, stap),
		uplinkTSPacket(2, first+3000, ssrc, true, paddedNAL(0x41, 200)),
	}
	got, _ := forwardUplinkVideoForTest(t, packets, nil, rebaseSetup(true, 1, false))
	if len(got) != 4 {
		t.Fatalf("forwarded %d packets, want 3 de-aggregated + 1", len(got))
	}
	for i := 0; i < 3; i++ {
		if got[i].Timestamp != DefaultSIPVideoTSBase {
			t.Fatalf("STAP-A NAL %d ts=%d, want %d", i, got[i].Timestamp, DefaultSIPVideoTSBase)
		}
	}
	if got[3].Timestamp != DefaultSIPVideoTSBase+3000 {
		t.Fatalf("P frame ts=%d, want base+3000", got[3].Timestamp)
	}
	assertUplinkRTPContinuity(t, got)
}

func TestUplinkTSRebaseOffPassesBrowserTimestamp(t *testing.T) {
	const first = uint32(4_200_000_000)
	packets := [][]byte{
		uplinkTSPacket(1, first, 1, false, []byte{0x67, 0x42, 0x00, 0x1f, 0xab}),
		uplinkTSPacket(2, first, 1, false, []byte{0x68, 0xce, 0x06, 0xe2}),
		uplinkTSPacket(3, first, 1, true, paddedNAL(0x65, 300)),
		uplinkTSPacket(4, first+3000, 1, true, paddedNAL(0x41, 200)),
	}
	got, sess := forwardUplinkVideoForTest(t, packets, nil, rebaseSetup(false, 0, false))
	if len(got) != 4 {
		t.Fatalf("forwarded %d packets, want 4", len(got))
	}
	for i, want := range []uint32{first, first, first, first + 3000} {
		if got[i].Timestamp != want {
			t.Fatalf("packet %d ts=%d, want browser ts %d", i, got[i].Timestamp, want)
		}
	}
	if sess.uplinkTSRebaser.started {
		t.Fatal("rebaser ran with SIP_VIDEO_TS_REBASE off")
	}
}

// Browser track replaced mid-call: new SSRC and an older random timestamp.
func TestUplinkTSRebaseBrowserSSRCChangeKeepsTimelineForward(t *testing.T) {
	packets := [][]byte{
		uplinkTSPacket(1, 2_000_000_000, 0xAAAA, false, []byte{0x67, 0x42, 0x00, 0x1f, 0xab}),
		uplinkTSPacket(2, 2_000_000_000, 0xAAAA, false, []byte{0x68, 0xce, 0x06, 0xe2}),
		uplinkTSPacket(3, 2_000_000_000, 0xAAAA, true, paddedNAL(0x65, 300)),
		uplinkTSPacket(4, 2_000_003_000, 0xAAAA, true, paddedNAL(0x41, 200)),
		uplinkTSPacket(500, 12345, 0xBBBB, true, paddedNAL(0x65, 300)),
		uplinkTSPacket(501, 15345, 0xBBBB, true, paddedNAL(0x41, 200)),
	}
	delays := make([]time.Duration, len(packets))
	delays[4] = 30 * time.Millisecond
	got, _ := forwardUplinkVideoForTest(t, packets, delays, rebaseSetup(true, 0, false))
	// 4 on the old SSRC; the new-SSRC IDR gets cached SPS/PPS injected.
	if len(got) != 8 {
		t.Fatalf("forwarded %d packets, want 4 + SPS/PPS/IDR + P", len(got))
	}
	if got[0].Timestamp != DefaultSIPVideoTSBase {
		t.Fatalf("first ts=%d", got[0].Timestamp)
	}
	for i := 1; i < len(got); i++ {
		if int32(got[i].Timestamp-got[i-1].Timestamp) < 0 {
			t.Fatalf("packet %d ts=%d went back from %d", i, got[i].Timestamp, got[i-1].Timestamp)
		}
	}
	idrTS := got[6].Timestamp
	if got[4].Payload[0]&0x1f != 7 || got[5].Payload[0]&0x1f != 8 || got[6].Payload[0]&0x1f != 5 ||
		got[4].Timestamp != idrTS || got[5].Timestamp != idrTS {
		t.Fatalf("new-SSRC IDR not preceded by SPS/PPS on its ts: %d/%d/%d", got[4].Timestamp, got[5].Timestamp, idrTS)
	}
	if int32(idrTS-got[3].Timestamp) <= 0 {
		t.Fatalf("new-SSRC IDR ts=%d not after the previous picture %d", idrTS, got[3].Timestamp)
	}
	if got[7].Timestamp-idrTS != 3000 {
		t.Fatalf("delta on the new SSRC = %d, want 3000", got[7].Timestamp-idrTS)
	}
	assertUplinkRTPContinuity(t, got)
}

func TestUplinkTSRebaseResetPerCall(t *testing.T) {
	s := &Session{ID: "rebase-reset", sipVideoTSRebase: true}
	s.mu.Lock()
	s.uplinkVideoOutboundTSLocked(4_000_000_000, 1, time.Now())
	s.uplinkVideoOutboundTSLocked(4_000_090_000, 1, time.Now())
	s.mu.Unlock()
	s.ResetMediaState()
	s.mu.Lock()
	defer s.mu.Unlock()
	if out := s.uplinkVideoOutboundTSLocked(77, 9, time.Now()); out != DefaultSIPVideoTSBase {
		t.Fatalf("new call first ts=%d, want base", out)
	}
}

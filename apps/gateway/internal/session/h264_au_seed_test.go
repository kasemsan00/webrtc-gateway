package session

import (
	"testing"
	"time"
)

func TestCacheSIPSPSPPSSeedsNormalizerDuringActiveGate(t *testing.T) {
	sess := &Session{
		ID:                      "seed-gate",
		VideoAUNormalizeEnabled: true,
		SwitchGeneration:        2,
	}
	sess.StartSwitchVideoGate(2, testSeedNow(), "switch")

	var seededSPS, seededPPS []byte
	sess.BindH264AUParameterSetSeeder(func(sps, pps []byte) {
		seededSPS = append([]byte(nil), sps...)
		seededPPS = append([]byte(nil), pps...)
	})
	t.Cleanup(func() { sess.BindH264AUParameterSetSeeder(nil) })

	sess.CacheSIPSPS([]byte{0x67, 0x42, 0x00, 0x1f})
	if len(seededSPS) != 0 || len(seededPPS) != 0 {
		t.Fatal("expected no seed until both parameter sets exist")
	}

	sess.CacheSIPPPS([]byte{0x68, 0xce, 0x06, 0xe2})
	if len(seededSPS) == 0 || len(seededPPS) == 0 {
		t.Fatal("expected normalizer seed after both parameter sets cached")
	}
	if seededSPS[0] != 0x67 || seededPPS[0] != 0x68 {
		t.Fatalf("unexpected seeded sets: sps=%v pps=%v", seededSPS, seededPPS)
	}
}

func TestCacheSIPSPSDoesNotSeedWhenGateInactive(t *testing.T) {
	sess := &Session{ID: "seed-inactive"}
	seeded := false
	sess.BindH264AUParameterSetSeeder(func(_, _ []byte) { seeded = true })
	t.Cleanup(func() { sess.BindH264AUParameterSetSeeder(nil) })

	sess.CacheSIPSPS([]byte{0x67, 0x42})
	sess.CacheSIPPPS([]byte{0x68, 0xce})
	if seeded {
		t.Fatal("expected no seed when switch gate inactive")
	}
}

func TestArmSwitchSPSPPSInjectIfAuthoritative(t *testing.T) {
	sess := &Session{SwitchGeneration: 4, MediaEpoch: 9}
	if !sess.ArmSwitchSPSPPSInjectIfAuthoritative(4, 9, 3) {
		t.Fatal("expected authoritative arm to succeed")
	}
	if sess.SwitchSPSPPSInjectRemaining != 3 {
		t.Fatalf("remaining = %d, want 3", sess.SwitchSPSPPSInjectRemaining)
	}
	if sess.ArmSwitchSPSPPSInjectIfAuthoritative(3, 9, 3) {
		t.Fatal("expected stale generation arm to fail")
	}
}

func TestSeedSIPCachedSPSPPSFromSDP_SeedsCacheAndLiveNormalizer(t *testing.T) {
	sess := &Session{ID: "seed-sdp"}
	var seededSPS, seededPPS []byte
	sess.BindH264AUParameterSetSeeder(func(sps, pps []byte) {
		seededSPS = append([]byte(nil), sps...)
		seededPPS = append([]byte(nil), pps...)
	})
	t.Cleanup(func() { sess.BindH264AUParameterSetSeeder(nil) })

	sps183 := []byte{0x67, 0x42, 0x00, 0x1f}
	pps183 := []byte{0x68, 0xce, 0x06, 0xe2}
	if !sess.SeedSIPCachedSPSPPSFromSDP(sps183, pps183, "sip-183") {
		t.Fatal("expected SDP seed to populate empty SIP cache")
	}
	gotSPS, gotPPS, ok := sess.GetSIPCachedSPSPPS()
	if !ok || len(gotSPS) != len(sps183) || len(gotPPS) != len(pps183) {
		t.Fatalf("unexpected cached sets: ok=%v sps=%x pps=%x", ok, gotSPS, gotPPS)
	}
	if len(seededSPS) != len(sps183) || len(seededPPS) != len(pps183) {
		t.Fatalf("expected live normalizer seeder to run even when switch gate inactive: sps=%x pps=%x", seededSPS, seededPPS)
	}

	// Identical SDP re-seed is a no-op.
	if sess.SeedSIPCachedSPSPPSFromSDP(sps183, pps183, "sip-183-dup") {
		t.Fatal("expected identical SDP seed to be a no-op")
	}

	// 200 OK SDP with different parameter sets may overwrite 183 SDP-seeded cache
	// while no live SIP RTP SPS/PPS has been seen yet.
	sps200 := []byte{0x67, 0x64, 0x00, 0x28}
	pps200 := []byte{0x68, 0xee, 0x3c, 0x80}
	if !sess.SeedSIPCachedSPSPPSFromSDP(sps200, pps200, "sip-200-ok") {
		t.Fatal("expected 200 OK SDP seed to overwrite 183 SDP-seeded SIP cache")
	}
	gotSPS, gotPPS, ok = sess.GetSIPCachedSPSPPS()
	if !ok || gotSPS[1] != 0x64 || gotPPS[1] != 0xee {
		t.Fatalf("expected 200 OK SDP sets to replace 183 sets, got sps=%x pps=%x", gotSPS, gotPPS)
	}
	if len(seededSPS) != len(sps200) || seededSPS[1] != 0x64 || seededPPS[1] != 0xee {
		t.Fatalf("expected live normalizer to be re-seeded with 200 OK sets, got sps=%x pps=%x", seededSPS, seededPPS)
	}

	// Once live SIP RTP populates SPS/PPS, SDP must NEVER overwrite it.
	rtpSPS := []byte{0x67, 0x4d, 0x00, 0x1e}
	rtpPPS := []byte{0x68, 0xeb, 0xe3, 0xcb}
	sess.CacheSIPSPS(rtpSPS)
	sess.CacheSIPPPS(rtpPPS)
	if sess.SeedSIPCachedSPSPPSFromSDP([]byte{0x67, 0x58, 0x00, 0x1f}, []byte{0x68, 0xaa, 0xbb, 0xcc}, "late-sdp") {
		t.Fatal("expected SDP seed not to overwrite live RTP-learned SIP cache")
	}
	gotSPS, gotPPS, _ = sess.GetSIPCachedSPSPPS()
	if gotSPS[1] != 0x4d || gotPPS[1] != 0xeb {
		t.Fatalf("live RTP SIP SPS/PPS was overwritten by SDP: sps=%x pps=%x", gotSPS, gotPPS)
	}

	// Clearing SIP video parameter sets (e.g. on @switch) resets RTP provenance so
	// fresh SDP seeding can populate the new stream again.
	sess.ClearSIPVideoParameterSets()
	if !sess.SeedSIPCachedSPSPPSFromSDP(sps200, pps200, "post-switch-sdp") {
		t.Fatal("expected SDP seed to succeed after ClearSIPVideoParameterSets")
	}
}

func TestPublicAccountRefOwnership_AtomicBindAndRelease(t *testing.T) {
	// Active session binds once and releases exactly once.
	sess := &Session{ID: "pub-1", State: StateConnecting}
	if !sess.BindPublicAccountKeyIfActive("userA@example.com:5060") {
		t.Fatal("expected BindPublicAccountKeyIfActive to succeed on active session")
	}
	if key := sess.TakePublicAccountKeyForRelease(); key != "userA@example.com:5060" {
		t.Fatalf("expected first TakePublicAccountKeyForRelease to return key, got %q", key)
	}
	if key := sess.TakePublicAccountKeyForRelease(); key != "" {
		t.Fatalf("expected second TakePublicAccountKeyForRelease to return empty, got %q", key)
	}
	if sess.BindPublicAccountKeyIfActive("userA@example.com:5060") {
		t.Fatal("expected BindPublicAccountKeyIfActive to fail after release")
	}

	// Refuses bind after PendingCancelRequested, StateEnded, or TerminalAction.
	for _, tc := range []struct {
		name  string
		setup func(*Session)
	}{
		{"pending_cancel", func(s *Session) { s.MarkPendingCancelRequested() }},
		{"state_ended", func(s *Session) { s.SetState(StateEnded) }},
		{"terminal_action", func(s *Session) { s.TryBeginTerminalAction("cancel") }},
		{"already_released", func(s *Session) { _ = s.TakePublicAccountKeyForRelease() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Session{ID: "pub-" + tc.name, State: StateConnecting}
			tc.setup(s)
			if s.BindPublicAccountKeyIfActive("userA@example.com:5060") {
				t.Fatalf("expected BindPublicAccountKeyIfActive to return false for %s", tc.name)
			}
			if key := s.TakePublicAccountKeyForRelease(); key != "" {
				t.Fatalf("expected empty release key for %s, got %q", tc.name, key)
			}
		})
	}
}

func testSeedNow() time.Time {
	return time.Unix(1_700_000_000, 0)
}

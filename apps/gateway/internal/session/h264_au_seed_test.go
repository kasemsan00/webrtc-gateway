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

func testSeedNow() time.Time {
	return time.Unix(1_700_000_000, 0)
}

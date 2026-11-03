package sip

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"webrtc-sip-gateway/internal/config"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	defer func() { os.Stdout = original }()

	fn()
	writer.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestTrunkRoutineCycleLogsRequireDebugFlag(t *testing.T) {
	tm := &TrunkManager{cfg: &config.Config{}}
	out := captureStdout(t, func() {
		tm.logTrunkInfoOrDebug(false, "📞 [TrunkManager] Acquired lease for trunk %d (until %s)\n", 7, "until")
		tm.logTrunkDebug("📞 [TrunkManager] Trunk %d REGISTER target=%s transport=%s contact=sip:%s@%s:%d\n", 7, "pbx:5060", "tcp", "1001", "203.0.113.10", 5060)
		tm.logTrunkDebug("📞 [TrunkManager] Trunk %d: Received %d, retrying with auth\n", 7, 401)
		tm.logTrunkInfoOrDebug(false, "✅ [TrunkManager] Trunk %d registered successfully (expires in %ds)\n", 7, 60)
		tm.logTrunkDebug("📞 [TrunkManager] Trunk %d: Refresh worker stopped\n", 7)
	})
	if strings.Contains(out, "trunk 7") {
		t.Fatalf("routine trunk lines printed without DEBUG_SIP_TRUNK: %q", out)
	}

	info := captureStdout(t, func() {
		tm.logTrunkInfoOrDebug(true, "📞 [TrunkManager] Acquired lease for trunk %d (until %s)\n", 7, "until")
		tm.logTrunkInfoOrDebug(true, "✅ [TrunkManager] Trunk %d registered successfully (expires in %ds)\n", 7, 60)
		tm.logTrunkDebug("📞 [TrunkManager] Trunk %d: Received %d, retrying with auth\n", 7, 401)
	})
	if !strings.Contains(info, "Acquired lease for trunk 7") || !strings.Contains(info, "registered successfully") {
		t.Fatalf("state-change lines missing: %q", info)
	}
	if strings.Contains(info, "Received 401") || strings.Contains(info, "retrying with auth") {
		t.Fatalf("expected 401 challenge is not an INFO line: %q", info)
	}

	tm.cfg.SIP.DebugSIPTrunk = true
	debug := captureStdout(t, func() {
		tm.logTrunkInfoOrDebug(false, "📞 [TrunkManager] Acquired lease for trunk %d (until %s)\n", 7, "until")
		tm.logTrunkDebug("📞 [TrunkManager] Trunk %d: Received %d, retrying with auth\n", 7, 401)
		tm.logTrunkDebug("📞 [TrunkManager] Trunk %d: Refresh worker stopped\n", 7)
	})
	for _, want := range []string{"Acquired lease for trunk 7", "Received 401", "Refresh worker stopped"} {
		if !strings.Contains(debug, want) {
			t.Fatalf("DEBUG_SIP_TRUNK missing %q in %q", want, debug)
		}
	}
}

func TestRegistrationAnnounceTracksFirstAndRecovery(t *testing.T) {
	tm := &TrunkManager{}
	if tm.registrationAnnounced[1] {
		t.Fatal("new trunk must not count as already announced")
	}

	tm.mu.Lock()
	tm.setRegistrationAnnouncedLocked(1, true)
	announced := tm.registrationAnnounced[1]
	tm.mu.Unlock()
	if !announced {
		t.Fatal("expected first success to be announced")
	}

	tm.mu.Lock()
	tm.setRegistrationAnnouncedLocked(1, false)
	_, still := tm.registrationAnnounced[1]
	tm.mu.Unlock()
	if still {
		t.Fatal("expected error or lease loss to clear announcement so recovery logs again")
	}
}

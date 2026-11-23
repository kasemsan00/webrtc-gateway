package session

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/pion/rtcp"
)

func TestSendWebRTCFeedback_FailedWriteRTCPDoesNotBurnPLIThrottle(t *testing.T) {
	var failWrite atomic.Bool
	failWrite.Store(true)
	var writes atomic.Int32

	sess := &Session{
		ID:        "pli-write-fail",
		VideoSSRC: 0x11223344,
		writeWebRTCRTCP: func(pkts []rtcp.Packet) error {
			writes.Add(1)
			if failWrite.Load() {
				return errors.New("DTLS transport has not started yet")
			}
			return nil
		},
	}

	// First kick fails before DTLS starts: must NOT burn LastWebRTCPLISent.
	sess.SendPLItoWebRTC()
	if writes.Load() != 1 {
		t.Fatalf("writes = %d, want 1", writes.Load())
	}
	if !sess.LastWebRTCPLISent.IsZero() {
		t.Fatalf("failed WriteRTCP stamped LastWebRTCPLISent=%v", sess.LastWebRTCPLISent)
	}

	// Once DTLS is ready, an immediate retry must NOT be throttled.
	failWrite.Store(false)
	sess.SendPLItoWebRTC()
	if writes.Load() != 2 {
		t.Fatalf("writes after recovery = %d, want 2", writes.Load())
	}
	if sess.LastWebRTCPLISent.IsZero() {
		t.Fatal("successful WriteRTCP did not stamp LastWebRTCPLISent")
	}

	// Subsequent immediate kick MUST now be throttled.
	sess.SendPLItoWebRTC()
	if writes.Load() != 2 {
		t.Fatalf("writes after throttle = %d, want 2", writes.Load())
	}
}

func TestSendWebRTCFeedback_FailedWriteRTCPDoesNotBurnFIRThrottleOrCounters(t *testing.T) {
	var failWrite atomic.Bool
	failWrite.Store(true)
	var writes atomic.Int32

	sess := &Session{
		ID:        "fir-write-fail",
		VideoSSRC: 0x55667788,
		writeWebRTCRTCP: func(pkts []rtcp.Packet) error {
			writes.Add(1)
			if failWrite.Load() {
				return errors.New("DTLS transport has not started yet")
			}
			return nil
		},
	}

	sess.SendFIRToWebRTC()
	if writes.Load() != 1 {
		t.Fatalf("writes = %d, want 1", writes.Load())
	}
	if !sess.LastWebRTCFIRSent.IsZero() || sess.FIRSeq != 0 || sess.PLISent != 0 {
		t.Fatalf("failed FIR mutated state: last=%v firSeq=%d pliSent=%d", sess.LastWebRTCFIRSent, sess.FIRSeq, sess.PLISent)
	}

	failWrite.Store(false)
	sess.SendFIRToWebRTC()
	if writes.Load() != 2 {
		t.Fatalf("writes after recovery = %d, want 2", writes.Load())
	}
	if sess.LastWebRTCFIRSent.IsZero() || sess.FIRSeq != 1 || sess.PLISent != 1 {
		t.Fatalf("successful FIR did not update state: last=%v firSeq=%d pliSent=%d", sess.LastWebRTCFIRSent, sess.FIRSeq, sess.PLISent)
	}

	// Immediate follow-up FIR must be throttled.
	sess.SendFIRToWebRTC()
	if writes.Load() != 2 {
		t.Fatalf("writes after throttle = %d, want 2", writes.Load())
	}
}

func TestSendProactiveAndBrowserPLI_FailedWriteRTCPDoesNotBurnThrottle(t *testing.T) {
	var failWrite atomic.Bool
	failWrite.Store(true)
	var writes atomic.Int32

	sess := &Session{
		ID:        "proactive-write-fail",
		VideoSSRC: 0x99aabbcc,
		writeWebRTCRTCP: func(pkts []rtcp.Packet) error {
			writes.Add(1)
			if failWrite.Load() {
				return errors.New("DTLS transport has not started yet")
			}
			return nil
		},
	}

	if sess.SendProactivePLIToWebRTC() {
		t.Fatal("SendProactivePLIToWebRTC returned true on WriteRTCP error")
	}
	if !sess.LastWebRTCPLISent.IsZero() || !sess.lastBrowserIDRRequest.IsZero() {
		t.Fatalf("failed proactive PLI stamped timestamps: pli=%v idrReq=%v", sess.LastWebRTCPLISent, sess.lastBrowserIDRRequest)
	}

	if sess.SendBrowserPLI("startup") {
		t.Fatal("SendBrowserPLI returned true on WriteRTCP error")
	}
	if !sess.LastWebRTCPLISent.IsZero() || !sess.lastBrowserIDRRequest.IsZero() {
		t.Fatalf("failed SendBrowserPLI stamped timestamps: pli=%v idrReq=%v", sess.LastWebRTCPLISent, sess.lastBrowserIDRRequest)
	}

	failWrite.Store(false)
	if !sess.SendBrowserPLI("startup") {
		t.Fatal("SendBrowserPLI after DTLS ready failed unexpectedly")
	}
	if sess.LastWebRTCPLISent.IsZero() || sess.lastBrowserIDRRequest.IsZero() {
		t.Fatalf("successful SendBrowserPLI did not stamp timestamps: pli=%v idrReq=%v", sess.LastWebRTCPLISent, sess.lastBrowserIDRRequest)
	}
	// Next call within 500ms/1s window must be coalesced/throttled.
	if sess.SendBrowserPLI("startup") {
		t.Fatal("SendBrowserPLI inside cooldown was not throttled")
	}
	if writes.Load() != 3 {
		t.Fatalf("writes = %d, want 3 (2 failed + 1 succeeded)", writes.Load())
	}
}

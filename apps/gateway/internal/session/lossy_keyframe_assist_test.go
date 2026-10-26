package session

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type lossyTestSession struct {
	*Session
	sends atomic.Int32
}

func newLossyTestSession(t *testing.T, assist bool) *lossyTestSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ls := &lossyTestSession{Session: &Session{
		ID:                        "lossy",
		ctx:                       ctx,
		cancel:                    cancel,
		lossyAssist:               assist,
		lossyThresholdPct:         5,
		lossyExtraDelay:           40 * time.Millisecond,
		lossyEarlyWindow:          10 * time.Second,
		lossySafety:               3 * time.Second,
		proactiveBrowserPLISafety: 10 * time.Second,
	}}
	ls.lossyExtraSend = func(string) bool {
		ls.sends.Add(1)
		return true
	}
	return ls
}

// feedSIPVideo sends seconds of 100 pps SIP video with lossPct of every
// 100 sequence numbers missing, and returns the next sequence and time.
func feedSIPVideo(s *Session, seq uint16, at time.Time, seconds, lossPct int) (uint16, time.Time) {
	for sec := 0; sec < seconds; sec++ {
		for i := 0; i < 100; i++ {
			seq++
			if i < lossPct {
				continue
			}
			s.NoteSIPVideoSeq(at.Add(time.Duration(i)*10*time.Millisecond), seq)
		}
		at = at.Add(time.Second)
	}
	seq++
	s.NoteSIPVideoSeq(at, seq) // roll the last bucket so it is evaluated
	return seq, at
}

func (ls *lossyTestSession) answer(at time.Time) {
	ls.mu.Lock()
	ls.noteCallAnsweredLocked(at)
	ls.mu.Unlock()
}

func (ls *lossyTestSession) waitExtraDone(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ls.mu.RLock()
		pending := ls.lossyExtraPending
		ls.mu.RUnlock()
		if !pending {
			time.Sleep(3 * ls.lossyExtraDelay)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("extra IDR request still pending")
}

func TestSIPVideoLossWindowCountsLossNotReorder(t *testing.T) {
	w := sipVideoLossWindow{}
	now := time.Now()
	// 0, 2, 1, 4, 3 ... every pair swapped: reordered, nothing lost.
	w.observe(now, 0)
	for i := uint16(0); i < 200; i += 2 {
		w.observe(now, i+2)
		w.observe(now, i+1)
	}
	w.observe(now.Add(time.Second), 201) // complete the bucket
	if exp, lost := w.totals(); exp != 201 || lost != 0 {
		t.Fatalf("reorder expected=%d lost=%d, want 201/0", exp, lost)
	}
	// Drop every tenth packet of the next 100.
	for i := uint16(202); i <= 301; i++ {
		if i%10 != 0 {
			w.observe(now.Add(time.Second), i)
		}
	}
	w.observe(now.Add(2*time.Second), 302)
	if exp, lost := w.totals(); exp != 302 || lost != 10 {
		t.Fatalf("loss expected=%d lost=%d, want 302/10", exp, lost)
	}
	// A large forward jump is a new stream, not 5000 lost packets.
	w.observe(now.Add(2*time.Second), 5300)
	w.observe(now.Add(3*time.Second), 5301)
	if _, lost := w.totals(); lost != 10 {
		t.Fatalf("stream rebase counted as loss: lost=%d", lost)
	}
}

func TestLossyAssistNonLossyLegUnchanged(t *testing.T) {
	ls := newLossyTestSession(t, true)
	now := time.Now()
	feedSIPVideo(ls.Session, 1000, now.Add(-7*time.Second), 6, 2)
	if lossy, pct := ls.SIPVideoLossy(); lossy {
		t.Fatalf("2%% leg marked lossy (pct=%.1f)", pct)
	}
	ls.answer(now.Add(-time.Second))
	ls.HandleSIPInfoPictureFastUpdate(now)
	ls.waitExtraDone(t)
	if n := ls.sends.Load(); n != 0 {
		t.Fatalf("non-lossy leg sent %d extra IDR requests", n)
	}
	ls.mu.RLock()
	safety, sipSeen := ls.proactiveSafetyForCallLocked(true)
	ls.mu.RUnlock()
	if safety != 10*time.Second || !sipSeen {
		t.Fatalf("safety=%s sipSeen=%v, want 10s and the answered-VFU skip kept", safety, sipSeen)
	}
}

func TestLossyAssistSchedulesOneExtraIDRInEarlyWindow(t *testing.T) {
	ls := newLossyTestSession(t, true)
	now := time.Now()
	feedSIPVideo(ls.Session, 1000, now.Add(-7*time.Second), 6, 11)
	if lossy, pct := ls.SIPVideoLossy(); !lossy || pct < 10 || pct > 12 {
		t.Fatalf("lossy=%v pct=%.1f, want lossy at ~11%%", lossy, pct)
	}
	ls.answer(now.Add(-2 * time.Second))
	ls.HandleSIPInfoPictureFastUpdate(now)
	// A second VFU past the debounce while the extra is still scheduled.
	ls.HandleSIPInfoPictureFastUpdate(now.Add(SIPInfoFIRDebounce + 10*time.Millisecond))
	ls.waitExtraDone(t)
	if n := ls.sends.Load(); n != 1 {
		t.Fatalf("extra IDR requests = %d, want exactly 1", n)
	}
}

func TestLossyAssistNoExtraIDRAfterEarlyWindow(t *testing.T) {
	ls := newLossyTestSession(t, true)
	now := time.Now()
	feedSIPVideo(ls.Session, 1000, now.Add(-7*time.Second), 6, 15)
	ls.answer(now.Add(-11 * time.Second))
	ls.HandleSIPInfoPictureFastUpdate(now)
	ls.waitExtraDone(t)
	if n := ls.sends.Load(); n != 0 {
		t.Fatalf("extra IDR requests after the early window = %d, want 0", n)
	}
}

func TestLossyAssistOffKeepsOldBehaviour(t *testing.T) {
	ls := newLossyTestSession(t, false)
	now := time.Now()
	feedSIPVideo(ls.Session, 1000, now.Add(-7*time.Second), 6, 20)
	if lossy, _ := ls.SIPVideoLossy(); lossy {
		t.Fatal("assist off still reports a lossy leg")
	}
	if ls.lossyLoss.haveBase {
		t.Fatal("assist off still tracks loss")
	}
	ls.answer(now.Add(-time.Second))
	ls.HandleSIPInfoPictureFastUpdate(now)
	ls.waitExtraDone(t)
	if n := ls.sends.Load(); n != 0 {
		t.Fatalf("assist off sent %d extra IDR requests", n)
	}
	ls.mu.RLock()
	safety, sipSeen := ls.proactiveSafetyForCallLocked(true)
	ls.mu.RUnlock()
	if safety != 10*time.Second || !sipSeen {
		t.Fatalf("safety=%s sipSeen=%v, want the old 10s / answered skip", safety, sipSeen)
	}
}

func TestLossyExtraIDRCoalescesWithPendingRequest(t *testing.T) {
	ls := newLossyTestSession(t, true)
	now := time.Now()
	ls.mu.Lock()
	ls.lossyExtraGen = 7
	if _, _, reason := ls.lossyExtraIDRDecisionLocked(6, now); reason != "stale" {
		t.Fatalf("old generation reason=%q, want stale", reason)
	}
	ls.sipInfoFIRRetryActive = true
	if send, _, reason := ls.lossyExtraIDRDecisionLocked(7, now); send || reason != "sip-retry-pending" {
		t.Fatalf("pending VFU retry send=%v reason=%q", send, reason)
	}
	ls.sipInfoFIRRetryActive = false
	ls.lastBrowserIDRRequest = now.Add(-300 * time.Millisecond)
	if send, wait, reason := ls.lossyExtraIDRDecisionLocked(7, now); send || wait != 0 || reason != "pli-pending" {
		t.Fatalf("unanswered PLI send=%v wait=%s reason=%q", send, wait, reason)
	}
	ls.uplinkVideoLastIDR = now.Add(-100 * time.Millisecond)
	ls.uplinkIDRInFlight = true
	if send, wait, reason := ls.lossyExtraIDRDecisionLocked(7, now); send || wait != lossyExtraIDRInFlightWait || reason != "idr-in-flight" {
		t.Fatalf("IDR in flight send=%v wait=%s reason=%q", send, wait, reason)
	}
	ls.uplinkIDRInFlight = false
	if send, wait, reason := ls.lossyExtraIDRDecisionLocked(7, now); send || reason != "min-gap" || wait != 400*time.Millisecond {
		t.Fatalf("right after an IDR send=%v wait=%s reason=%q, want min-gap 400ms", send, wait, reason)
	}
	if send, _, _ := ls.lossyExtraIDRDecisionLocked(7, now.Add(400*time.Millisecond)); !send {
		t.Fatal("extra request not sent once Chrome would act on it")
	}
	ls.mu.Unlock()

	// End to end: a VFU retry still pending when the extra fires absorbs it.
	ls2 := newLossyTestSession(t, true)
	feedSIPVideo(ls2.Session, 1000, now.Add(-7*time.Second), 6, 10)
	ls2.answer(now.Add(-time.Second))
	ls2.HandleSIPInfoPictureFastUpdate(now)
	ls2.mu.Lock()
	ls2.sipInfoFIRRetryActive = true
	ls2.mu.Unlock()
	ls2.waitExtraDone(t)
	if n := ls2.sends.Load(); n != 0 {
		t.Fatalf("extra IDR sent on top of a pending VFU retry (%d)", n)
	}
}

func TestLossySafetyIntervalSwitchesAndRecovers(t *testing.T) {
	ls := newLossyTestSession(t, true)
	wake := ls.ProactivePLIWakeChan()
	start := time.Now().Add(-20 * time.Second)
	seq, at := feedSIPVideo(ls.Session, 1000, start, 6, 10)
	select {
	case <-wake:
	default:
		t.Fatal("lossy transition did not wake the proactive loop")
	}

	// IDR 4s ago, VFU answered by it: normal legs skip, lossy legs send.
	now := time.Now()
	ls.mu.Lock()
	ls.uplinkVideoLastIDR = now.Add(-4 * time.Second)
	ls.LastUplinkKeyframe = ls.uplinkVideoLastIDR
	ls.sipKeyframeRequestSeen = true
	ls.sipKeyframeRequestAt = now.Add(-4500 * time.Millisecond)
	safety, _ := ls.proactiveSafetyForCallLocked(true)
	ls.mu.Unlock()
	if safety != 3*time.Second {
		t.Fatalf("lossy safety=%s, want 3s", safety)
	}
	if d := ls.DecideSessionProactiveBrowserPLI(now); !d.Send || d.Reason != "safety-net" {
		t.Fatalf("lossy decision=%+v, want safety-net send", d)
	}

	_, _ = feedSIPVideo(ls.Session, seq, at, 6, 0)
	if lossy, _ := ls.SIPVideoLossy(); lossy {
		t.Fatal("leg still lossy after 6s without loss")
	}
	select {
	case <-wake:
	default:
		t.Fatal("recovery did not wake the proactive loop")
	}
	ls.mu.RLock()
	safety, sipSeen := ls.proactiveSafetyForCallLocked(true)
	ls.mu.RUnlock()
	if safety != 10*time.Second || !sipSeen {
		t.Fatalf("recovered safety=%s sipSeen=%v, want 10s and the answered skip", safety, sipSeen)
	}
	if d := ls.DecideSessionProactiveBrowserPLI(now); d.Send || d.Reason != "idr-fresh" {
		t.Fatalf("recovered decision=%+v, want idr-fresh skip", d)
	}
}

func TestResetMediaStateClearsLossyAssist(t *testing.T) {
	ls := newLossyTestSession(t, true)
	feedSIPVideo(ls.Session, 1000, time.Now().Add(-7*time.Second), 6, 10)
	ls.answer(time.Now())
	ls.ResetMediaState()
	if lossy, _ := ls.SIPVideoLossy(); lossy || !ls.lossyAnsweredAt.IsZero() || ls.lossyLoss.haveBase {
		t.Fatal("new call kept the previous call's lossy state")
	}
}

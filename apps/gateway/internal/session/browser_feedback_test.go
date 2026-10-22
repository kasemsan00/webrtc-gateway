package session

import (
	"testing"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
)

func TestBrowserRTCPFeedbackLine(t *testing.T) {
	feedback := newBrowserFeedback("s", true)
	feedback.noteReports([]rtcp.Packet{&rtcp.ReceiverReport{Reports: []rtcp.ReceptionReport{{
		FractionLost: 20,
		TotalLost:    7,
	}}}})
	feedback.mu.Lock()
	feedback.videoRTP = 270
	feedback.twccSent = 4
	feedback.twccReceived = 800
	feedback.twccLost = 3
	feedback.mu.Unlock()

	line, ok := feedback.snapshot(time.Unix(1_700_000_000, 0))
	if !ok {
		t.Fatal("snapshot was suppressed")
	}
	want := "browser_rtcp_feedback rr_sent=1 rr_fraction_lost=20 rr_cumulative_lost=7 twcc_sent=4 twcc_received=800 twcc_lost=3 browser_rtp_per_s=270"
	if line != want {
		t.Fatalf("line %q", line)
	}
}

func TestTWCCFeedbackCountsASequenceGapAsLoss(t *testing.T) {
	feedback := newBrowserFeedback("s", true)
	feedback.mu.Lock()
	feedback.recorder.Record(9, 1, 0)
	feedback.recorder.Record(9, 2, 1000)
	feedback.recorder.Record(9, 4, 3000)
	packets := feedback.recorder.BuildFeedbackPacket()
	feedback.mu.Unlock()

	received, lost := twccLossCounts(packets)
	if received != 3 || lost < 1 {
		t.Fatalf("twcc received=%d lost=%d, want received 3 and a gap", received, lost)
	}
}

func TestBrowserFeedbackObserveDoesNotWaitForRTCPWrite(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	writer := interceptor.RTCPWriterFunc(func(pkts []rtcp.Packet, _ interceptor.Attributes) (int, error) {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		return len(pkts), nil
	})
	feedback := newBrowserFeedback("stall", true)
	defer func() {
		close(release)
		_ = feedback.Close()
	}()
	feedback.BindRTCPWriter(writer)
	feedback.mu.Lock()
	feedback.recorder.Record(9, 1, 1000)
	feedback.mu.Unlock()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("TWCC write did not start")
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			feedback.observe(9, 0, true, nil, nil)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("video RTP observe blocked while RTCP write was stuck")
	}
}

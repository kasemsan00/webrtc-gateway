package session

import (
	"testing"
	"time"
)

func TestRecordKeyframeResponseTimeOnlyForPendingSIPRequest(t *testing.T) {
	for _, kind := range []string{"none", "expired", "browser-fir", "pending-sip"} {
		t.Run(kind, func(t *testing.T) {
			s := &Session{ID: kind}
			switch kind {
			case "expired":
				s.PLISent, s.LastSipPLISent = 1, time.Now().Add(-time.Minute)
				s.LastPLISent = s.LastSipPLISent
			case "browser-fir":
				s.PLISent, s.LastPLISent = 1, time.Now().Add(-time.Millisecond)
			case "pending-sip":
				s.PLISent, s.LastSipPLISent = 1, time.Now().Add(-10*time.Millisecond)
				s.LastPLISent = s.LastSipPLISent
			}
			response, elapsed, _, responses := s.RecordKeyframe()
			if response != (kind == "pending-sip") {
				t.Fatalf("response=%v", response)
			}
			if response {
				if elapsed <= 0 || elapsed >= 2*time.Second || responses != 1 {
					t.Fatalf("elapsed=%v responses=%d", elapsed, responses)
				}
			} else if elapsed != 0 || responses != 0 {
				t.Fatalf("unsolicited IDR reported latency=%v responses=%d", elapsed, responses)
			}
			response, elapsed, _, responses2 := s.RecordKeyframe()
			if response || elapsed != 0 || responses2 != responses {
				t.Fatalf("same request counted twice: response=%v elapsed=%v responses=%d", response, elapsed, responses2)
			}
		})
	}
}

package session

import (
	"fmt"
	"strings"
	"time"
)

const (
	// StartupUplinkKeyframeFresh is how long startup/periodic browser PLIs
	// stay quiet after a complete uplink IDR was forwarded.
	StartupUplinkKeyframeFresh = 1500 * time.Millisecond
	// DecoderJoinUplinkKeyframeFresh skips dest-ready / ssrc-learn /
	// sip-rtcp-first kicks when a complete IDR is this recent.
	DecoderJoinUplinkKeyframeFresh = 10 * time.Second
	// SIPKeyframePLICoalesce is the minimum gap between SIP INFO/FIR
	// browser PLIs. Repeats wait for this window instead of stacking IDRs.
	SIPKeyframePLICoalesce = time.Second
)

// DecideUplinkKeyframeRequest is the fake-clock gate for browser IDR
// requests. Switch always sends. Startup/periodic skip while an IDR is
// in flight or within StartupUplinkKeyframeFresh of a complete IDR.
// Decoder-join kicks use a 10s complete-IDR window. SIP INFO/FIR still
// honour the phone: they wait out an in-flight IDR and coalesce to 1/s.
func DecideUplinkKeyframeRequest(kind string, inFlight bool, completeAt, lastPLI, now time.Time) (bool, string) {
	switch {
	case isSwitchUplinkKeyframeKind(kind):
		return true, ""
	case isDecoderJoinKeyframeKind(kind):
		if inFlight {
			return false, "idr-in-flight"
		}
		if uplinkIDRCompleteWithin(completeAt, now, DecoderJoinUplinkKeyframeFresh) {
			return false, "idr-fresh"
		}
		return true, ""
	case isSIPUplinkKeyframeKind(kind):
		if inFlight {
			return false, "idr-in-flight"
		}
		if !lastPLI.IsZero() && now.Sub(lastPLI) < SIPKeyframePLICoalesce && now.Sub(lastPLI) >= 0 {
			return false, "coalesced"
		}
		return true, ""
	default:
		if inFlight {
			return false, "idr-in-flight"
		}
		if uplinkIDRCompleteWithin(completeAt, now, StartupUplinkKeyframeFresh) {
			return false, "idr-fresh"
		}
		return true, ""
	}
}

func isSwitchUplinkKeyframeKind(kind string) bool {
	return kind == "switch" || kind == "implicit-switch"
}

func isDecoderJoinKeyframeKind(kind string) bool {
	switch kind {
	case "sip-rtcp-first", "sip-video-dest-ready", "remote-ssrc-learn", "sip-video-rtp-learn":
		return true
	default:
		return strings.HasPrefix(kind, "bridged-")
	}
}

func isSIPUplinkKeyframeKind(kind string) bool {
	switch kind {
	case "sip_info_fir", "sip_info_fir_retry", "pli", "fir":
		return true
	default:
		return false
	}
}

func uplinkIDRCompleteWithin(completeAt, now time.Time, window time.Duration) bool {
	if completeAt.IsZero() || window <= 0 {
		return false
	}
	age := now.Sub(completeAt)
	return age >= 0 && age < window
}

func (s *Session) uplinkIDRGateLocked() (inFlight bool, completeAt, lastPLI time.Time) {
	return s.uplinkIDRInFlight, s.uplinkIDRCompleteAt, s.lastBrowserIDRRequest
}

// DecideSessionUplinkKeyframeRequest is DecideUplinkKeyframeRequest using session clocks.
func (s *Session) DecideSessionUplinkKeyframeRequest(kind string, now time.Time) (bool, string) {
	if s == nil {
		return true, ""
	}
	s.mu.RLock()
	inFlight, completeAt, lastPLI := s.uplinkIDRGateLocked()
	s.mu.RUnlock()
	return DecideUplinkKeyframeRequest(kind, inFlight, completeAt, lastPLI, now)
}

// SkipUplinkKeyframeRequest logs and reports that this kind of browser IDR
// request should not be sent now.
func (s *Session) SkipUplinkKeyframeRequest(kind string, now time.Time) (bool, string) {
	send, reason := s.DecideSessionUplinkKeyframeRequest(kind, now)
	if send {
		return false, ""
	}
	fmt.Printf("[%s] uplink_keyframe_skipped kind=%s reason=%s\n", s.ID, kind, reason)
	return true, reason
}

func (s *Session) noteUplinkIDRProgressLocked(isIDR bool, rtpTimestamp uint32, marker bool) {
	if !isIDR {
		return
	}
	newPicture := !s.uplinkVideoIDRTimestampSeen || rtpTimestamp != s.uplinkVideoLastIDRTimestamp
	if newPicture || !s.uplinkIDRInFlight {
		s.uplinkIDRInFlight = true
		s.uplinkIDRInFlightTS = rtpTimestamp
	}
	if marker {
		s.uplinkIDRInFlight = false
		s.uplinkIDRCompleteAt = s.LastUplinkKeyframe
		if s.uplinkIDRCompleteAt.IsZero() {
			s.uplinkIDRCompleteAt = time.Now()
		}
	}
}

// UplinkIDRInFlight reports that an IDR picture has started toward SIP
// and its marker has not been written yet.
func (s *Session) UplinkIDRInFlight() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.uplinkIDRInFlight
}

// HasCompleteUplinkIDR reports that at least one full IDR picture was forwarded.
func (s *Session) HasCompleteUplinkIDR() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.uplinkIDRCompleteAt.IsZero()
}

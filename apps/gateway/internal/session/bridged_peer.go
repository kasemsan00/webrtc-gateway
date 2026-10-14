package session

import (
	"fmt"
	"strings"
	"time"
)

func bridgedSIPUser(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if start := strings.Index(value, "<"); start >= 0 {
		if end := strings.Index(value[start+1:], ">"); end >= 0 {
			value = value[start+1 : start+1+end]
		}
	}
	value = strings.TrimPrefix(strings.TrimPrefix(value, "sip:"), "sips:")
	if at := strings.Index(value, "@"); at >= 0 {
		value = value[:at]
	}
	if colon := strings.Index(value, ":"); colon >= 0 {
		value = value[:colon]
	}
	return strings.TrimSpace(value)
}

// FindBridgedGatewayPeer returns the other WebRTC session on a queue-bridged
// call. An outbound caller maps to the inbound agent with the same caller
// username, and an inbound agent maps back to that outbound caller.
func FindBridgedGatewayPeer(sessions []*Session, origin *Session) *Session {
	if origin == nil {
		return nil
	}
	dir, from, _, _ := origin.GetCallInfo()
	dir = strings.ToLower(strings.TrimSpace(dir))
	user := bridgedSIPUser(from)
	if user == "" {
		return nil
	}
	want := "inbound"
	if dir == "inbound" {
		want = "outbound"
	} else if dir != "outbound" {
		return nil
	}
	for _, sess := range sessions {
		if sess == nil || sess.ID == origin.ID || sess.GetState() == StateEnded {
			continue
		}
		peerDir, peerFrom, _, _ := sess.GetCallInfo()
		if !strings.EqualFold(strings.TrimSpace(peerDir), want) {
			continue
		}
		if bridgedSIPUser(peerFrom) == user {
			return sess
		}
	}
	return nil
}

// RequestBridgedBrowserKeyframe sends a forced PLI/FIR burst to this session's
// WebRTC peer. It ignores uplink-idr-fresh suppression because the bridged
// decoder is a new destination. Requests closer than
// sipOriginatedBrowserKeyframeMinInterval are skipped.
func (s *Session) RequestBridgedBrowserKeyframe(reason string, now time.Time) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	if !s.bridgedKeyframeAt.IsZero() {
		age := now.Sub(s.bridgedKeyframeAt)
		if age >= 0 && age < sipOriginatedBrowserKeyframeMinInterval {
			s.mu.Unlock()
			fmt.Printf("[%s] bridged_keyframe_skipped reason=rate-limit trigger=%s\n", s.ID, reason)
			return false
		}
	}
	s.bridgedKeyframeAt = now
	s.mu.Unlock()
	s.KickUplinkKeyframeForSIPDecoder("bridged-" + reason)
	return true
}

// RelayBridgedKeyframe forwards a keyframe request to the bridged gateway
// session's browser. Asterisk does not relay PLI/FIR between the two legs.
func RelayBridgedKeyframe(sessions []*Session, origin *Session, reason string, now time.Time) (*Session, bool) {
	peer := FindBridgedGatewayPeer(sessions, origin)
	if peer == nil {
		return nil, false
	}
	if !peer.RequestBridgedBrowserKeyframe(reason, now) {
		return peer, false
	}
	if origin != nil {
		fmt.Printf("[%s] bridged_keyframe_relay peer=%s reason=%s\n", origin.ID, peer.ID, reason)
	}
	return peer, true
}

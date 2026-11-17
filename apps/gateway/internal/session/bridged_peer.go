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
//
// The caller username is only a guess: one caller can have several calls (a
// second line, call waiting). The pairing is therefore made only when it is
// unambiguous both ways, or when the @switch agent of the outbound call names
// the agent session. Otherwise it returns nil, so hanging up or relaying for one
// call never reaches the agent of another call from the same caller.
func FindBridgedGatewayPeer(sessions []*Session, origin *Session) *Session {
	if origin == nil {
		return nil
	}
	dir, from, _, _ := origin.GetCallInfo()
	dir = strings.ToLower(strings.TrimSpace(dir))
	user := bridgedSIPUser(from)
	if user == "" || (dir != "inbound" && dir != "outbound") {
		return nil
	}
	candidates := bridgedCandidates(sessions, origin, user)
	var linked []*Session
	for _, sess := range candidates {
		if bridgedBySwitchAgent(origin, sess) {
			linked = append(linked, sess)
		}
	}
	if len(linked) == 1 {
		return linked[0]
	}
	if len(candidates) != 1 {
		return nil
	}
	peer := candidates[0]
	for _, other := range bridgedCandidates(sessions, peer, user) {
		if other.ID != origin.ID {
			// The peer could belong to another call of the same caller.
			return nil
		}
	}
	return peer
}

// bridgedCandidates lists live sessions in the opposite direction of origin
// whose caller username is user.
func bridgedCandidates(sessions []*Session, origin *Session, user string) []*Session {
	dir, _, _, _ := origin.GetCallInfo()
	want := "inbound"
	if strings.EqualFold(strings.TrimSpace(dir), "inbound") {
		want = "outbound"
	}
	var out []*Session
	for _, sess := range sessions {
		if sess == nil || sess.ID == origin.ID || sess.GetState() == StateEnded {
			continue
		}
		peerDir, peerFrom, _, _ := sess.GetCallInfo()
		if strings.EqualFold(strings.TrimSpace(peerDir), want) && bridgedSIPUser(peerFrom) == user {
			out = append(out, sess)
		}
	}
	return out
}

// bridgedBySwitchAgent reports whether the outbound one of a and b was
// switched (@switch) to the agent whose SIP user owns the inbound one.
func bridgedBySwitchAgent(a, b *Session) bool {
	outbound, inbound := a, b
	if dir, _, _, _ := a.GetCallInfo(); strings.EqualFold(strings.TrimSpace(dir), "inbound") {
		outbound, inbound = b, a
	}
	agent := bridgedSIPUser(outbound.ChatMessageDestination())
	if agent == "" {
		return false
	}
	_, _, _, _, sipUser, _, _ := inbound.GetSIPAuthContext()
	return bridgedSIPUser(sipUser) == agent
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

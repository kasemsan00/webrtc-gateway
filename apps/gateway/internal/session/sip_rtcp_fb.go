package session

import (
	"fmt"
	"strings"
	"time"

	"webrtc-sip-gateway/internal/config"
)

// SIPKeyframeFeedbackInput is the fake-clock view of a SIP-leg PLI/FIR attempt.
type SIPKeyframeFeedbackInput struct {
	Mode             string
	Kind             string
	InFlight         bool
	CompleteAt       time.Time
	LastRTCP         time.Time
	PacketSeen       bool
	LastPacketMarker bool
	Guard            time.Duration
	MinInterval      time.Duration
	Now              time.Time
}

// SIPKeyframeFeedbackDecision says whether this PLI/FIR may go out as RTCP,
// should be redirected to SIP INFO picture_fast_update, or must wait for the
// next forwarded marker bit (so Asterisk's forced marker lands on a new frame).
type SIPKeyframeFeedbackDecision struct {
	SendRTCP    bool
	SendINFO    bool
	PendingRTCP bool
	Reason      string
}

// NormalizeSIPKeyframeRequestMode maps env/session values onto info|rtcp|both.
// Empty (zero-value sessions in unit tests) keeps the legacy RTCP path.
func NormalizeSIPKeyframeRequestMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case config.SIPKeyframeRequestModeRTCP:
		return config.SIPKeyframeRequestModeRTCP
	case config.SIPKeyframeRequestModeBoth:
		return config.SIPKeyframeRequestModeBoth
	case config.SIPKeyframeRequestModeInfo:
		return config.SIPKeyframeRequestModeInfo
	case "":
		return config.SIPKeyframeRequestModeRTCP
	default:
		return config.SIPKeyframeRequestModeInfo
	}
}

func sipKeyframeRTCPKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "pli", "fir":
		return true
	default:
		return false
	}
}

func sipKeyframeWantsRTCP(mode string) bool {
	switch NormalizeSIPKeyframeRequestMode(mode) {
	case config.SIPKeyframeRequestModeRTCP, config.SIPKeyframeRequestModeBoth:
		return true
	default:
		return false
	}
}

func sipKeyframeWantsINFO(mode string) bool {
	switch NormalizeSIPKeyframeRequestMode(mode) {
	case config.SIPKeyframeRequestModeInfo, config.SIPKeyframeRequestModeBoth:
		return true
	default:
		return false
	}
}

// DecideSIPKeyframeFeedback is the fake-clock gate for RTCP PLI/FIR toward the
// SIP/Asterisk video leg. Asterisk stamps marker=1 on the next forwarded
// packet after each PLI/FIR, so RTCP is skipped while an uplink IDR is in
// flight, during the post-IDR guard, faster than MinInterval, and mid-frame.
func DecideSIPKeyframeFeedback(in SIPKeyframeFeedbackInput) SIPKeyframeFeedbackDecision {
	if !sipKeyframeRTCPKind(in.Kind) {
		return SIPKeyframeFeedbackDecision{SendRTCP: true}
	}
	wantRTCP := sipKeyframeWantsRTCP(in.Mode)
	wantINFO := sipKeyframeWantsINFO(in.Mode)
	if !wantRTCP {
		return SIPKeyframeFeedbackDecision{SendINFO: wantINFO, Reason: "mode-info"}
	}
	if in.InFlight {
		return SIPKeyframeFeedbackDecision{SendINFO: wantINFO, Reason: "idr-in-flight"}
	}
	if in.Guard > 0 && uplinkIDRCompleteWithin(in.CompleteAt, in.Now, in.Guard) {
		return SIPKeyframeFeedbackDecision{SendINFO: wantINFO, Reason: "idr-guard"}
	}
	if in.MinInterval > 0 && !in.LastRTCP.IsZero() {
		age := in.Now.Sub(in.LastRTCP)
		if age >= 0 && age < in.MinInterval {
			return SIPKeyframeFeedbackDecision{SendINFO: wantINFO, Reason: "rate-limit"}
		}
	}
	if in.PacketSeen && !in.LastPacketMarker {
		return SIPKeyframeFeedbackDecision{SendINFO: wantINFO, PendingRTCP: true, Reason: "mid-frame"}
	}
	return SIPKeyframeFeedbackDecision{SendRTCP: true, SendINFO: wantINFO}
}

func (s *Session) sipKeyframeFeedbackInputLocked(kind string, now time.Time) SIPKeyframeFeedbackInput {
	lastRTCP := s.LastSipPLISent
	if s.LastSipFIRSent.After(lastRTCP) {
		lastRTCP = s.LastSipFIRSent
	}
	return SIPKeyframeFeedbackInput{
		Mode:             s.sipKeyframeRequestMode,
		Kind:             kind,
		InFlight:         s.uplinkIDRInFlight,
		CompleteAt:       s.uplinkIDRCompleteAt,
		LastRTCP:         lastRTCP,
		PacketSeen:       s.uplinkVideoPacketSeen,
		LastPacketMarker: s.lastUplinkPacketMarker,
		Guard:            s.sipRTCPFBGuard,
		MinInterval:      s.sipRTCPFBMinInterval,
		Now:              now,
	}
}

func (s *Session) queuePendingSIPRTCPLocked(kind, trigger string, force bool, authority *switchFeedbackAuthority) {
	s.pendingSIPRTCPKind = kind
	s.pendingSIPRTCPTrigger = trigger
	s.pendingSIPRTCPForce = force
	s.pendingSIPRTCPHasAuthority = authority != nil
	if authority != nil {
		s.pendingSIPRTCPGeneration = authority.generation
		s.pendingSIPRTCPMediaEpoch = authority.mediaEpoch
	} else {
		s.pendingSIPRTCPGeneration = 0
		s.pendingSIPRTCPMediaEpoch = 0
	}
}

func (s *Session) takePendingSIPRTCPLocked() (kind, trigger string, force bool, authority *switchFeedbackAuthority, ok bool) {
	if s.pendingSIPRTCPKind == "" {
		return "", "", false, nil, false
	}
	kind = s.pendingSIPRTCPKind
	trigger = s.pendingSIPRTCPTrigger
	force = s.pendingSIPRTCPForce
	if s.pendingSIPRTCPHasAuthority {
		authority = &switchFeedbackAuthority{s.pendingSIPRTCPGeneration, s.pendingSIPRTCPMediaEpoch}
	}
	s.pendingSIPRTCPKind = ""
	s.pendingSIPRTCPTrigger = ""
	s.pendingSIPRTCPForce = false
	s.pendingSIPRTCPHasAuthority = false
	s.pendingSIPRTCPGeneration = 0
	s.pendingSIPRTCPMediaEpoch = 0
	return kind, trigger, force, authority, true
}

func (s *Session) noteUplinkForwardMarkerLocked(marker bool) (kind, trigger string, force bool, authority *switchFeedbackAuthority, flush bool) {
	s.uplinkVideoPacketSeen = true
	s.lastUplinkPacketMarker = marker
	if !marker {
		return "", "", false, nil, false
	}
	kind, trigger, force, authority, ok := s.takePendingSIPRTCPLocked()
	return kind, trigger, force, authority, ok
}

// SetSIPKeyframeINFOSender installs the in-dialog INFO sender used when
// SIP_KEYFRAME_REQUEST_MODE is info or both.
func (s *Session) SetSIPKeyframeINFOSender(fn func(reason string)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.sipKeyframeINFO = fn
	s.mu.Unlock()
}

func (s *Session) requestSIPKeyframeINFO(reason string) {
	if s == nil {
		return
	}
	if strings.TrimSpace(reason) == "" {
		reason = "info"
	}
	if !s.AllowSwitchVideoInfoFIR(time.Now()) {
		fmt.Printf("[%s] sip_keyframe_via_info_skipped reason=rate-limit trigger=%s\n", s.ID, reason)
		return
	}
	s.mu.RLock()
	fn := s.sipKeyframeINFO
	id := s.ID
	s.mu.RUnlock()
	if fn == nil {
		fmt.Printf("[%s] sip_keyframe_via_info_skipped reason=no-sender trigger=%s\n", id, reason)
		return
	}
	fmt.Printf("[%s] sip_keyframe_via_info trigger=%s\n", id, reason)
	go fn(reason)
}

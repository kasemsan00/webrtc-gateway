package api

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
)

// StartSwitchVideoRenegotiation begins client-assisted WebRTC renegotiation
// after @switch video gate release to reset the remote decoder binding.
func (s *Server) StartSwitchVideoRenegotiation(sessionID string, generation int) {
	if s.sessionMgr == nil || s.runtimeConfig == nil {
		return
	}
	if !s.runtimeConfig.SIP.SwitchVideoRenegotiateEnable || !s.runtimeConfig.SIP.MidCallRenegotiationEnable {
		return
	}

	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok || sess == nil {
		return
	}
	if !sess.TryClaimSwitchVideoRenegotiation(generation) {
		return
	}

	offerSDP, err := sess.CreatePeerConnectionOffer()
	if err != nil {
		sess.ReleaseSwitchVideoRenegotiationClaim(generation)
		fmt.Printf("[%s] switch_renegotiate_offer_failed generation=%d error=%v\n", sessionID, generation, err)
		return
	}

	started, ok := sess.TryBeginMidCallRenegotiation(session.MidCallRenegotiationRequest{
		Source:    session.MidCallRenegotiationSourceSwitchGateRelease,
		Method:    "WS",
		OfferSDP:  offerSDP,
		Reason:    "agent_switch",
		StartedAt: time.Now(),
	})
	if !ok {
		sess.ReleaseSwitchVideoRenegotiationClaim(generation)
		fmt.Printf("[%s] switch_renegotiate_pending_blocked generation=%d\n", sessionID, generation)
		return
	}

	s.NotifySwitchVideoRenegotiation(sessionID, started)
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sessionID,
		Category:  "ws",
		Name:      "switch_video_renegotiation_started",
		Data: map[string]interface{}{
			"renegotiationId": started.ID,
			"generation":      generation,
		},
	})
	log.Printf("[%s] switch_renegotiate_started generation=%d renegotiationId=%s", sessionID, generation, started.ID)
}

func (s *Server) NotifySwitchVideoRenegotiation(sessionID string, renegotiation session.MidCallRenegotiationSnapshot) {
	timeout := renegotiation.Timeout
	if timeout <= 0 {
		timeout = defaultMidCallRenegotiationTimeout
	}
	s.scheduleMidCallRenegotiationTimeout(sessionID, renegotiation.ID, timeout)

	s.mu.RLock()
	client := s.wsClients[sessionID]
	s.mu.RUnlock()
	if client == nil {
		return
	}

	s.sendWSMessage(client, WSMessage{
		Type:            "renegotiate",
		SessionID:       sessionID,
		RenegotiationID: renegotiation.ID,
		Reason:          "agent_switch",
		SDP:             renegotiation.OfferSDP,
		MediaDirection:  "sendrecv",
		HasVideo:        strconv.FormatBool(true),
		RequiresAnswer:  true,
		Status:          "pending",
	})
}

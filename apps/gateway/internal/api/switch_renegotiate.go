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
// when an authoritative @switch MESSAGE is accepted. The WS `renegotiate`
// is sent immediately so the client can createOffer in parallel with gateway
// PeerConnection prepare. Android cannot answer a mid-call remote offer.
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

	started, ok := sess.TryBeginMidCallRenegotiation(session.MidCallRenegotiationRequest{
		Source:    session.MidCallRenegotiationSourceSwitchMessage,
		Method:    "WS",
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

	if err := sess.PrepareSwitchPeerConnection(s.turnConfig, s.config.DebugTURN); err != nil {
		fmt.Printf("[%s] switch_renegotiate_prepare_failed generation=%d error=%v\n", sessionID, generation, err)
		_ = sess.FailMidCallRenegotiation(started.ID, 500, err.Error())
		sess.ReleaseSwitchVideoRenegotiationClaim(generation)
		s.notifySwitchVideoRenegotiationFailed(sessionID, started.ID, err.Error())
		return
	}
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
		MediaDirection:  "sendrecv",
		HasVideo:        strconv.FormatBool(true),
		RequiresAnswer:  true,
		Status:          "pending",
	})
}

func (s *Server) notifySwitchVideoRenegotiationFailed(sessionID, renegotiationID, reason string) {
	s.mu.RLock()
	client := s.wsClients[sessionID]
	s.mu.RUnlock()
	if client == nil {
		return
	}
	s.sendWSMessage(client, WSMessage{
		Type:            "renegotiate_result",
		SessionID:       sessionID,
		RenegotiationID: renegotiationID,
		Status:          "failed",
		Reason:          reason,
	})
}

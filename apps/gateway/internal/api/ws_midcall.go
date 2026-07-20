package api

import (
	"fmt"
	"log"
	"strings"
	"time"

	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
)

const (
	defaultMidCallRenegotiationTimeout = 10 * time.Second
)

func (s *Server) handleWSRenegotiateAnswer(client *WSClient, msg WSMessage) {
	sessionID := msg.SessionID
	if sessionID == "" {
		sessionID = client.sessionID
	}
	if sessionID == "" {
		s.sendWSError(client, "", "Session ID required for renegotiation answer")
		return
	}
	if msg.RenegotiationID == "" {
		s.sendWSError(client, sessionID, "Renegotiation ID required")
		return
	}
	if s.sessionMgr == nil {
		s.sendWSError(client, sessionID, "Session manager not available")
		return
	}

	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok || sess == nil {
		s.sendWSError(client, sessionID, "Session not found")
		return
	}

	status := strings.ToLower(strings.TrimSpace(msg.Status))
	if status == "" {
		status = "ok"
	}

	pending, hasPending := sess.GetPendingMidCallRenegotiation()
	if !hasPending || pending.ID != msg.RenegotiationID {
		s.sendWSError(client, sessionID, "Renegotiation not pending or mismatched")
		return
	}

	var completed bool
	switch status {
	case "ok":
		if pending.Source == session.MidCallRenegotiationSourceSwitchGateRelease {
			if err := sess.ApplyPeerConnectionAnswer(msg.SDP); err != nil {
				_ = sess.FailMidCallRenegotiation(msg.RenegotiationID, 488, err.Error())
				s.sendWSMessage(client, WSMessage{
					Type:            "renegotiate_result",
					SessionID:       sessionID,
					RenegotiationID: msg.RenegotiationID,
					Status:          "failed",
					Reason:          err.Error(),
				})
				s.logEvent(&logstore.Event{
					Timestamp: time.Now(),
					SessionID: sessionID,
					Category:  "ws",
					Name:      "ws_midcall_renegotiation_answer",
					Data: map[string]interface{}{
						"renegotiationId": msg.RenegotiationID,
						"status":          "failed",
						"reason":          err.Error(),
						"source":          pending.Source,
					},
				})
				return
			}
		}
		completed = sess.CompleteMidCallRenegotiation(msg.RenegotiationID, msg.SDP)
	case "failed":
		completed = sess.FailMidCallRenegotiation(msg.RenegotiationID, 488, msg.Reason)
	default:
		s.sendWSError(client, sessionID, "Unsupported renegotiation answer status")
		return
	}
	if !completed {
		s.sendWSError(client, sessionID, "Renegotiation not pending or mismatched")
		return
	}

	resultStatus := "ok"
	if status == "failed" {
		resultStatus = "failed"
	}
	s.sendWSMessage(client, WSMessage{
		Type:            "renegotiate_result",
		SessionID:       sessionID,
		RenegotiationID: msg.RenegotiationID,
		Status:          resultStatus,
		Reason:          msg.Reason,
	})
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sessionID,
		Category:  "ws",
		Name:      "ws_midcall_renegotiation_answer",
		Data: map[string]interface{}{
			"renegotiationId": msg.RenegotiationID,
			"status":          resultStatus,
			"reason":          msg.Reason,
		},
	})
}

func (s *Server) scheduleMidCallRenegotiationTimeout(sessionID, renegotiationID string, timeout time.Duration) {
	if timeout <= 0 {
		return
	}

	time.AfterFunc(timeout, func() {
		if s.sessionMgr == nil {
			return
		}
		sess, ok := s.sessionMgr.GetSession(sessionID)
		if !ok || sess == nil {
			return
		}
		if !sess.FailMidCallRenegotiation(renegotiationID, 408, "client_timeout") {
			return
		}

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
			Status:          "timeout",
			Reason:          "client_timeout",
		})
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sessionID,
			Category:  "ws",
			Name:      "ws_midcall_renegotiation_timeout",
			Data: map[string]interface{}{
				"renegotiationId": renegotiationID,
				"status":          "timeout",
				"reason":          "client_timeout",
			},
		})
	})
}

func (s *Server) handleWSClientState(client *WSClient, msg WSMessage) {
	availability := normalizeClientAvailability(msg.Availability)
	callState := normalizeClientCallState(msg.CallState)

	s.mu.Lock()
	client.availability = availability
	client.callState = callState
	s.mu.Unlock()
	s.notifyWSClientChanged("updated", client)

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: msg.SessionID,
		Category:  "ws",
		Name:      "ws_client_state",
		Data: map[string]interface{}{
			"availability": availability,
			"callState":    callState,
			"sessionId":    msg.SessionID,
		},
	})
}

// handleWSPing handles WebSocket ping messages
func (s *Server) handleWSPing(client *WSClient, _ WSMessage) {
	if s.config.DebugWebSocket {
		fmt.Printf("[WebSocket] 💓 Received ping from client (sessionID=%s)\n", client.sessionID)
	}
	response := WSMessage{
		Type: "pong",
	}
	s.sendWSMessage(client, response)
	if s.config.DebugWebSocket {
		fmt.Printf("[WebSocket] 💚 Sent pong to client (sessionID=%s)\n", client.sessionID)
	}
}

// handleWSRequestKeyframe handles explicit keyframe requests from clients.
// Used by resume/video-recovery flows to trigger fast FIR/PLI toward SIP side.
func (s *Server) handleWSRequestKeyframe(client *WSClient, msg WSMessage) {
	sessionID := msg.SessionID
	if sessionID == "" {
		sessionID = client.sessionID
	}
	if sessionID == "" {
		s.sendWSError(client, "", "Session ID required for keyframe request")
		return
	}

	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok {
		s.sendWSError(client, sessionID, "Session not found")
		return
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_request_keyframe",
	})

	if s.config.DebugWebSocket {
		log.Printf("📸 Keyframe request received: session=%s", sessionID)
	}

	action := sess.SendBrowserRecoveryToAsterisk("ws-request_keyframe")
	log.Printf("📈 request_keyframe_handled session=%s action=%s", sessionID, action)
}

package api

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"k2-gateway/internal/session"
)

// handleWSMessage processes WebSocket messages
func (s *Server) handleWSMessage(client *WSClient, message []byte) {
	var msg WSMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		s.sendWSError(client, "", "Invalid message format")
		return
	}

	if client != nil && client.agentOnly {
		if ok, reason := s.allowAgentWSMessage(client, msg); !ok {
			log.Printf("Agent WebSocket message rejected: type=%s sessionID=%s reason=%s", msg.Type, msg.SessionID, reason)
			s.sendWSError(client, msg.SessionID, reason)
			return
		}
	} else if client != nil && client.publicOnly {
		if ok, reason := s.allowPublicWSMessage(client, msg); !ok {
			log.Printf("Public WebSocket message rejected: type=%s sessionID=%s reason=%s", msg.Type, msg.SessionID, reason)
			s.sendWSError(client, msg.SessionID, reason)
			return
		}
	}

	switch msg.Type {
	case "offer":
		s.handleWSoffer(client, msg)
	case "ice":
		s.handleWSIce(client, msg)
	case "call":
		s.handleWSCall(client, msg)
	case "hangup":
		s.handleWSHangup(client, msg)
	case "dtmf":
		s.handleWSDTMF(client, msg)
	case "accept":
		s.handleWSAccept(client, msg)
	case "reject":
		s.handleWSReject(client, msg)
	case "ping":
		s.handleWSPing(client, msg)
	case "send_message":
		s.handleWSSendMessage(client, msg)
	case "resume":
		s.handleWSResume(client, msg)
	case "request_keyframe":
		s.handleWSRequestKeyframe(client, msg)
	case "renegotiate_answer":
		s.handleWSRenegotiateAnswer(client, msg)
	case "trunk_resolve":
		s.handleWSTrunkResolve(client, msg)
	case "trunk_push_token":
		s.handleWSTrunkPushToken(client, msg)
	case "agent_register":
		s.handleWSAgentRegister(client, msg)
	case "client_state":
		s.handleWSClientState(client, msg)
	case "translate":
		s.handleWSTranslate(client, msg)
	case "translate_stop":
		s.handleWSTranslateStop(client, msg)
	default:
		s.sendWSError(client, msg.SessionID, "Unknown message type")
	}
}

func (s *Server) allowAgentWSMessage(client *WSClient, msg WSMessage) (bool, string) {
	switch msg.Type {
	case "agent_register", "offer", "ping", "client_state":
		return true, ""
	case "call", "ice", "hangup", "accept", "reject", "dtmf", "request_keyframe", "renegotiate_answer":
		return true, ""
	case "trunk_push_token", "trunk_resolve", "resume":
		return false, fmt.Sprintf("Message type %q is not allowed on agent WebSocket", msg.Type)
	default:
		return false, fmt.Sprintf("Message type %q is not allowed on agent WebSocket", msg.Type)
	}
}

func (s *Server) allowPublicWSMessage(client *WSClient, msg WSMessage) (bool, string) {
	switch msg.Type {
	case "offer", "ping":
		return true, ""
	case "call":
		if msg.TrunkID > 0 || strings.TrimSpace(msg.TrunkPublicID) != "" {
			return false, "Trunk calls require authenticated WebSocket"
		}
		if strings.TrimSpace(msg.SIPDomain) == "" ||
			strings.TrimSpace(msg.SIPUsername) == "" ||
			strings.TrimSpace(msg.SIPPassword) == "" {
			return false, "Public SIP credentials required on public WebSocket"
		}
		return s.publicClientOwnsSession(client, msg, false)
	case "ice", "hangup", "dtmf", "request_keyframe", "media_health":
		return s.publicClientOwnsSession(client, msg, false)
	case "renegotiate_answer":
		return s.publicClientOwnsSession(client, msg, true)
	case "translate", "translate_stop", "send_message":
		return s.publicClientOwnsSession(client, msg, true)
	case "resume":
		return s.publicClientCanResume(client, msg)
	default:
		return false, fmt.Sprintf("Message type %q requires authenticated WebSocket", msg.Type)
	}
}

func (s *Server) publicClientOwnsSession(client *WSClient, msg WSMessage, requirePublic bool) (bool, string) {
	sessionID := strings.TrimSpace(msg.SessionID)
	if sessionID == "" && client != nil {
		sessionID = strings.TrimSpace(client.sessionID)
	}
	if sessionID == "" {
		return false, "Session ID required"
	}
	if client == nil || strings.TrimSpace(client.sessionID) == "" {
		return false, "Public WebSocket session is not established"
	}
	if sessionID != client.sessionID {
		return false, "Public WebSocket can only access its own session"
	}
	if s.sessionMgr == nil {
		return false, "Session manager not available"
	}
	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok {
		return false, "Session not found"
	}
	if requirePublic && !sessionIsPublic(sess) {
		return false, "Public WebSocket can only access public SIP sessions"
	}
	return true, ""
}

func (s *Server) publicClientCanResume(client *WSClient, msg WSMessage) (bool, string) {
	sessionID := strings.TrimSpace(msg.SessionID)
	if sessionID == "" {
		return false, "Session ID required for resume"
	}
	if client != nil && strings.TrimSpace(client.sessionID) != "" && sessionID != client.sessionID {
		return false, "Public WebSocket can only access its own session"
	}
	if s.sessionMgr == nil {
		return false, "Session manager not available"
	}
	if sess, ok := s.sessionMgr.GetSession(sessionID); ok && !sessionIsPublic(sess) {
		return false, "Public WebSocket can only resume public SIP sessions"
	}
	return true, ""
}

func sessionIsPublic(sess *session.Session) bool {
	if sess == nil {
		return false
	}
	mode, _, _, _, _, _, _ := sess.GetSIPAuthContext()
	return mode == "public"
}

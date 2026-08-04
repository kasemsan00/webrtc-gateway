package api

import (
	"fmt"

	"k2-gateway/internal/session"
)

type sipHoldController interface {
	SetHold(sess *session.Session, held bool) error
}

func (s *Server) handleWSHold(client *WSClient, msg WSMessage) {
	if msg.SessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}
	sess, ok := s.sessionMgr.GetSession(msg.SessionID)
	if !ok || sess == nil {
		s.sendWSError(client, msg.SessionID, "Session not found")
		return
	}
	held := msg.Type == "hold"
	if sess.IsHeld() == held {
		s.sendHoldState(client, msg.SessionID, held)
		return
	}
	controller, ok := s.sipMaker.(sipHoldController)
	if !ok || controller == nil {
		s.sendWSError(client, msg.SessionID, "SIP hold is not available")
		return
	}
	if err := controller.SetHold(sess, held); err != nil {
		s.sendWSError(client, msg.SessionID, fmt.Sprintf("Failed to update hold state: %v", err))
		return
	}
	s.sendHoldState(client, msg.SessionID, held)
}

func (s *Server) sendHoldState(client *WSClient, sessionID string, held bool) {
	value := held
	s.sendWSMessage(client, WSMessage{
		Type:      "hold_state",
		SessionID: sessionID,
		Held:      &value,
	})
}

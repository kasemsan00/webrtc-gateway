package api

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"k2-gateway/internal/session"
)

// NotifySessionState notifies WebSocket clients about session state changes
func (s *Server) NotifySessionState(sessionID string, state session.SessionState) {
	s.NotifySessionStateWithReason(sessionID, state, "session-state")
}

// NotifySessionStateWithReason notifies WebSocket clients and logs the trigger reason.
func (s *Server) NotifySessionStateWithReason(sessionID string, state session.SessionState, reason string) {
	if reason == "" {
		reason = "session-state"
	}

	// Notify session stream listeners about state changes
	var eventType string
	switch state {
	case session.StateConnecting:
		eventType = "session_created"
	case session.StateRinging:
		eventType = "session_state_changed"
	case session.StateActive:
		eventType = "session_active"
	case session.StateEnded:
		eventType = "session_ended"
	default:
		eventType = "session_state_changed"
	}
	sid := sessionID
	s.notifySessionListChanged(eventType, &sid)

	// Notify trunk stream listeners if trunk mode
	if s.sessionMgr != nil && (state == session.StateActive || state == session.StateEnded) {
		if sess, ok := s.sessionMgr.GetSession(sessionID); ok {
			authMode, _, trunkID, _, _, _, _ := sess.GetSIPAuthContext()
			if authMode == "trunk" && trunkID > 0 {
				tid := trunkID
				s.notifyTrunkListChanged("session_updated", &tid)
			}
		}
	}

	// Notify WebSocket client
	s.mu.RLock()
	client, ok := s.wsClients[sessionID]
	s.mu.RUnlock()

	if !ok {
		return
	}

	msg := WSMessage{
		Type:      "state",
		SessionID: sessionID,
		State:     string(state),
		Reason:    reason,
	}
	log.Printf("[%s] 📡 WS call-progress type=state state=%s reason=%s", sessionID, state, reason)
	s.sendWSMessage(client, msg)
	if state == session.StateEnded {
		s.unbindClientSession(client, sessionID)
	}

	// Additive ringing message for softphone-kmp-sdk RingingMessage compatibility.
	if state == session.StateRinging {
		log.Printf("[%s] 📡 WS call-progress type=ringing reason=%s", sessionID, reason)
		s.sendWSMessage(client, WSMessage{
			Type:      "ringing",
			SessionID: sessionID,
		})
	}
}

// NotifyRemoteMedia notifies the bound WebSocket client that remote SIP media is receiving.
// Missing clients and full send buffers are non-fatal (sendWSMessage drops with a log).
func (s *Server) NotifyRemoteMedia(sessionID, kind, direction, state string) {
	if kind == "" {
		kind = "video"
	}
	if direction == "" {
		direction = "remote"
	}
	if state == "" {
		state = "receiving"
	}

	s.mu.RLock()
	client, ok := s.wsClients[sessionID]
	s.mu.RUnlock()
	if !ok || client == nil {
		return
	}

	log.Printf("[%s] 📡 WS media kind=%s direction=%s state=%s", sessionID, kind, direction, state)
	s.sendWSMessage(client, WSMessage{
		Type:      "media",
		SessionID: sessionID,
		Kind:      kind,
		Direction: direction,
		State:     state,
	})
}

func (s *Server) NotifyMidCallRenegotiation(sessionID string, renegotiation session.MidCallRenegotiationSnapshot, validation session.MidCallSDPValidation) {
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

	mediaDirection := validation.Video.Direction
	if mediaDirection == "" {
		mediaDirection = validation.Audio.Direction
	}
	reason := "renegotiate"
	if validation.HasActiveVideo {
		reason = "video_added"
	} else if validation.Video.Present && (validation.Video.Port == 0 || validation.Video.Direction == "inactive") {
		reason = "video_removed"
	}

	s.sendWSMessage(client, WSMessage{
		Type:            "renegotiate",
		SessionID:       sessionID,
		RenegotiationID: renegotiation.ID,
		Reason:          reason,
		SDP:             renegotiation.OfferSDP,
		MediaDirection:  mediaDirection,
		HasVideo:        strconv.FormatBool(validation.HasActiveVideo),
		RequiresAnswer:  true,
		Status:          "pending",
	})
}

// handleWSSendMessage handles WebSocket send_message requests
func (s *Server) handleWSSendMessage(client *WSClient, msg WSMessage) {
	if msg.Body == "" {
		s.sendWSError(client, "", "Message body required")
		return
	}

	// Default content type
	contentType := msg.ContentType
	if contentType == "" {
		contentType = "text/plain;charset=UTF-8"
	}

	// Send SIP MESSAGE
	if s.sipMaker != nil {
		// Try to find an active session for this client to send in-dialog message
		var sess *session.Session
		if client.sessionID != "" {
			sess, _ = s.sessionMgr.GetSession(client.sessionID)
		}

		// If we have a session with remote contact, use in-dialog messaging
		if sess != nil {
			_, _, remoteContact, _, _, _, _ := sess.GetSIPDialogState()
			if remoteContact != "" {
				log.Printf("💬 Sending in-dialog message via session %s to %s", sess.ID, remoteContact)
				if err := s.sipMaker.SendMessageToSession(sess, msg.Body, contentType); err != nil {
					s.sendWSError(client, "", fmt.Sprintf("Failed to send in-dialog message: %v", err))
					return
				}
				return
			}
		}

		// Fallback to out-of-dialog message - requires destination
		if msg.Destination == "" {
			s.sendWSError(client, "", "No active session and no destination specified")
			return
		}
		if err := s.sipMaker.SendMessage(msg.Destination, msg.From, msg.Body, contentType); err != nil {
			s.sendWSError(client, "", fmt.Sprintf("Failed to send message: %v", err))
			return
		}
	}

	// Send confirmation to client
	response := WSMessage{
		Type:        "messageSent",
		Destination: msg.Destination,
		Body:        msg.Body,
	}
	s.sendWSMessage(client, response)
	log.Printf("💬 Message sent successfully")
}

func sipURIUsername(value string) string {
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

func sipAddressMatches(target, targetUser, candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return false
	}
	if target != "" && candidate == target {
		return true
	}
	candidateUser := sipURIUsername(candidate)
	return targetUser != "" && candidateUser == targetUser
}

func (s *Server) findSIPMessageSessionID(to string) string {
	if s.sessionMgr == nil {
		return ""
	}

	targetUser := sipURIUsername(to)
	for _, sess := range s.sessionMgr.ListSessions() {
		if sess == nil || sess.GetState() == session.StateEnded {
			continue
		}
		_, fromField, toField, _ := sess.GetCallInfo()
		_, _, _, _, sipUsername, _, _ := sess.GetSIPAuthContext()
		if sipAddressMatches(to, targetUser, fromField) ||
			sipAddressMatches(to, targetUser, toField) ||
			sipAddressMatches(to, targetUser, sipUsername) {
			return sess.ID
		}
	}

	return ""
}

func (s *Server) selectSIPMessageTargets(sessionID string) (targets []*WSClient, totalConnections int, droppedDuplicate int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	totalConnections = len(s.wsConnections)
	seen := make(map[*WSClient]struct{})
	addTarget := func(client *WSClient) {
		if client == nil {
			return
		}
		if _, ok := seen[client]; ok {
			droppedDuplicate++
			return
		}
		seen[client] = struct{}{}
		targets = append(targets, client)
	}

	if sessionID == "" {
		return targets, totalConnections, droppedDuplicate
	}

	if client := s.wsClients[sessionID]; client != nil {
		addTarget(client)
		for key, mapped := range s.wsClients {
			if key == sessionID {
				continue
			}
			if mapped == client || (mapped != nil && mapped.sessionID == sessionID) {
				droppedDuplicate++
			}
		}
		return targets, totalConnections, droppedDuplicate
	}

	var newest *WSClient
	for client := range s.wsConnections {
		if client == nil || client.sessionID != sessionID {
			continue
		}
		if newest == nil || client.ConnectedAt.After(newest.ConnectedAt) {
			newest = client
		}
	}
	addTarget(newest)
	return targets, totalConnections, droppedDuplicate
}

// NotifySIPMessage notifies the WebSocket client associated with an incoming SIP message.
func (s *Server) NotifySIPMessage(to, from, body, contentType string) {
	sessionID := s.findSIPMessageSessionID(to)
	targets, totalConnections, droppedDuplicate := s.selectSIPMessageTargets(sessionID)

	msg := WSMessage{
		Type:        "message",
		SessionID:   sessionID,
		From:        from,
		To:          to,
		Body:        body,
		ContentType: contentType,
	}

	for _, client := range targets {
		s.sendWSMessage(client, msg)
		log.Printf("💬 Sent message notification to client (sessionID=%s targetSessionID=%s)", client.sessionID, sessionID)
	}

	if len(targets) == 0 {
		if sessionID == "" {
			log.Printf("⚠️ SIP message has no matching active session: from=%s to=%s totalConnections=%d", from, to, totalConnections)
		} else {
			log.Printf("⚠️ No WebSocket client connected for SIP message: from=%s to=%s targetSessionID=%s totalConnections=%d", from, to, sessionID, totalConnections)
		}
	}

	filtered := totalConnections - len(targets)
	if filtered < 0 {
		filtered = 0
	}
	log.Printf("💬 SIP message fanout summary: from=%s to=%s targetSessionID=%s recipients=%d filtered=%d duplicates=%d total=%d bodyBytes=%d", from, to, sessionID, len(targets), filtered, droppedDuplicate, totalConnections, len(body))
}

// NotifyDTMF notifies the WebSocket client about a received DTMF digit from SIP side
func (s *Server) NotifyDTMF(sessionID, digit string) {
	s.mu.RLock()
	client, ok := s.wsClients[sessionID]
	s.mu.RUnlock()

	if !ok {
		log.Printf("📞 DTMF received for session %s but no WebSocket client found", sessionID)
		return
	}

	log.Printf("📞 Forwarding DTMF '%s' to WebSocket client (sessionID=%s)", digit, sessionID)

	msg := WSMessage{
		Type:      "dtmf",
		SessionID: sessionID,
		Digits:    digit,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal DTMF notification: %v", err)
		return
	}

	select {
	case client.send <- data:
		log.Printf("📞 DTMF '%s' sent to client (sessionID=%s)", digit, sessionID)
	default:
		log.Printf("Failed to send DTMF notification - client channel full")
	}
}

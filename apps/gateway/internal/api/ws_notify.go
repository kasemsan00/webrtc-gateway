package api

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/sip"
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

	// Notify WebSocket client. Prefer the public /ws-public binding when an
	// agent connection has overwritten wsClients[sessionID].
	targets, _, _ := s.selectSIPMessageTargets(sessionID)
	if len(targets) == 0 {
		return
	}

	msg := WSMessage{
		Type:      "state",
		SessionID: sessionID,
		State:     string(state),
		Reason:    reason,
	}
	log.Printf("[%s] 📡 WS call-progress type=state state=%s reason=%s", sessionID, state, reason)
	for _, client := range targets {
		s.sendWSMessage(client, msg)
		if state == session.StateEnded {
			s.unbindClientSession(client, sessionID)
		}
	}

	// Additive ringing message for softphone-kmp-sdk RingingMessage compatibility.
	if state == session.StateRinging {
		log.Printf("[%s] 📡 WS call-progress type=ringing reason=%s", sessionID, reason)
		for _, client := range targets {
			s.sendWSMessage(client, WSMessage{
				Type:      "ringing",
				SessionID: sessionID,
			})
		}
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
		s.sendWSOperationError(client, msg.SessionID, "send_message", "Message body required")
		return
	}

	// Default content type
	contentType := msg.ContentType
	if contentType == "" {
		contentType = "text/plain;charset=UTF-8"
	}

	// Send SIP MESSAGE
	if s.sipMaker != nil {
		// Prefer the explicitly addressed session. Agent connections require this
		// value because client.sessionID is only a legacy projection in multi-call mode.
		var sess *session.Session
		sessionID := strings.TrimSpace(msg.SessionID)
		if sessionID == "" && !client.isAgentPresence() {
			sessionID = client.sessionID
		}
		if sessionID != "" {
			sess, _ = s.sessionMgr.GetSession(sessionID)
		}

		// Route session chat through the PBX. Asterisk is a B2BUA and may accept
		// an in-dialog MESSAGE without forwarding it to the remote call leg.
		if sess != nil {
			if !client.isAgentPresence() {
				if dest := strings.TrimSpace(msg.Destination); dest != "" {
					sess.SetMessageRemoteUser(sipURIUsername(dest))
				}
				_, callFrom, _, _ := sess.GetCallInfo()
				from := sipURIUsername(callFrom)
				if from == "" {
					from = strings.TrimSpace(msg.From)
				}
				agentSess := s.findBridgedAgentSession(sess)
				if agentSess != nil && s.relayChatToSession(agentSess, from, msg.Body, contentType) {
					dest := sess.ChatMessageDestination()
					if dest == "" {
						_, _, _, _, dest, _, _ = agentSess.GetSIPAuthContext()
						dest = sipURIUsername(dest)
					}
					s.sendWSMessage(client, WSMessage{
						Type:        "messageSent",
						SessionID:   sessionID,
						Destination: dest,
						Body:        msg.Body,
					})
					return
				}
			}
			var err error
			if client.isAgentPresence() && sess.HasDialogState() {
				log.Printf("💬 Sending in-dialog message via session %s", sess.ID)
				err = s.sipMaker.SendMessageToSession(sess, msg.Body, contentType)
			} else {
				log.Printf("💬 Sending PBX-routed message via session %s", sess.ID)
				err = s.sipMaker.SendMessageForSession(sess, msg.Body, contentType)
			}
			if err != nil {
				log.Printf("⚠️ SIP MESSAGE failed for session %s: %v", sessionID, err)
				s.sendWSOperationError(client, sessionID, "send_message", fmt.Sprintf("Failed to send session message: %v", err))
				return
			}
			if client.isAgentPresence() {
				_, _, _, _, agentUser, _, _ := sess.GetSIPAuthContext()
				from := sipURIUsername(agentUser)
				if from == "" {
					from = strings.TrimSpace(msg.From)
				}
				if publicSess := s.findBridgedPublicSession(sess); publicSess != nil {
					s.relayChatToSession(publicSess, from, msg.Body, contentType)
				}
			}
			s.sendWSMessage(client, WSMessage{
				Type:        "messageSent",
				SessionID:   sessionID,
				Destination: msg.Destination,
				Body:        msg.Body,
			})
			log.Printf("💬 Message sent successfully via PBX for session %s", sessionID)
			return
		}
		if client.isAgentPresence() {
			s.sendWSOperationError(client, sessionID, "send_message", "Active SIP session required for agent message")
			return
		}

		// Fallback to out-of-dialog message - requires destination
		if msg.Destination == "" {
			s.sendWSOperationError(client, sessionID, "send_message", "No active session and no destination specified")
			return
		}
		if err := s.sipMaker.SendMessage(msg.Destination, msg.From, msg.Body, contentType); err != nil {
			s.sendWSOperationError(client, sessionID, "send_message", fmt.Sprintf("Failed to send message: %v", err))
			return
		}
	}

	// Send confirmation to client
	response := WSMessage{
		Type:        "messageSent",
		SessionID:   msg.SessionID,
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

func (s *Server) findAgentChatSession(agentUser string) *session.Session {
	agentUser = sipURIUsername(agentUser)
	if agentUser == "" || s.sessionMgr == nil {
		return nil
	}
	for _, sess := range s.sessionMgr.ListSessions() {
		if sess == nil || sess.GetState() == session.StateEnded {
			continue
		}
		_, _, _, _, sipUser, _, _ := sess.GetSIPAuthContext()
		if sipURIUsername(sipUser) == agentUser {
			return sess
		}
	}
	return nil
}

func (s *Server) findBridgedAgentSession(publicSess *session.Session) *session.Session {
	if publicSess == nil || s.sessionMgr == nil {
		return nil
	}
	if found := s.findAgentChatSession(publicSess.ChatMessageDestination()); found != nil && found.ID != publicSess.ID {
		return found
	}
	_, pubFrom, _, _ := publicSess.GetCallInfo()
	pubUser := sipURIUsername(pubFrom)
	if pubUser == "" {
		return nil
	}
	for _, sess := range s.sessionMgr.ListSessions() {
		if sess == nil || sess.ID == publicSess.ID || sess.GetState() == session.StateEnded {
			continue
		}
		dir, fromField, _, _ := sess.GetCallInfo()
		if !strings.EqualFold(strings.TrimSpace(dir), "inbound") {
			continue
		}
		if sipURIUsername(fromField) == pubUser {
			return sess
		}
	}
	return nil
}

func (s *Server) findBridgedPublicSession(agentSess *session.Session) *session.Session {
	if agentSess == nil || s.sessionMgr == nil {
		return nil
	}
	dir, fromField, _, _ := agentSess.GetCallInfo()
	if !strings.EqualFold(strings.TrimSpace(dir), "inbound") {
		return nil
	}
	caller := sipURIUsername(fromField)
	if caller == "" {
		return nil
	}
	for _, sess := range s.sessionMgr.ListSessions() {
		if sess == nil || sess.ID == agentSess.ID || sess.GetState() == session.StateEnded {
			continue
		}
		pubDir, pubFrom, _, _ := sess.GetCallInfo()
		if !strings.EqualFold(strings.TrimSpace(pubDir), "outbound") {
			continue
		}
		if sipURIUsername(pubFrom) == caller {
			return sess
		}
	}
	return nil
}

func (s *Server) relayChatToSession(agentSess *session.Session, from, body, contentType string) bool {
	if agentSess == nil {
		return false
	}
	targets, _, _ := s.selectSIPMessageTargets(agentSess.ID)
	if len(targets) == 0 {
		log.Printf("⚠️ Public chat agent session %s has no WebSocket client", agentSess.ID)
		return false
	}
	_, _, _, _, agentUser, _, _ := agentSess.GetSIPAuthContext()
	agentUser = sipURIUsername(agentUser)
	msg := WSMessage{
		Type:        "message",
		SessionID:   agentSess.ID,
		From:        from,
		To:          agentUser,
		Body:        body,
		ContentType: contentType,
	}
	for _, client := range targets {
		s.sendWSMessage(client, msg)
	}
	log.Printf("💬 Relayed chat to session %s localUser=%s recipients=%d", agentSess.ID, agentUser, len(targets))
	return true
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

func sessionLocalChatIdentities(sess *session.Session) (sipUsername, localParty string) {
	dir, fromField, toField, _ := sess.GetCallInfo()
	_, _, _, _, sipUsername, _, _ = sess.GetSIPAuthContext()
	localParty = fromField
	if strings.EqualFold(strings.TrimSpace(dir), "inbound") {
		localParty = toField
	}
	return sipUsername, localParty
}

func (s *Server) findSIPMessageSessionID(to string) string {
	if s.sessionMgr == nil {
		return ""
	}

	targetUser := sipURIUsername(to)
	var bySIPUser, byLocalParty string
	for _, sess := range s.sessionMgr.ListSessions() {
		if sess == nil || sess.GetState() == session.StateEnded {
			continue
		}
		sipUsername, localParty := sessionLocalChatIdentities(sess)
		if bySIPUser == "" && sipAddressMatches(to, targetUser, sipUsername) {
			bySIPUser = sess.ID
		}
		if byLocalParty == "" && sipAddressMatches(to, targetUser, localParty) {
			byLocalParty = sess.ID
		}
	}
	if bySIPUser != "" {
		return bySIPUser
	}
	return byLocalParty
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

	mapped := s.wsClients[sessionID]
	preferred := mapped
	if mapped != nil && mapped.isAgentPresence() {
		var publicClient *WSClient
		for client := range s.wsConnections {
			if client == nil || client.isAgentPresence() {
				continue
			}
			if !wsClientMatchesSession(client, sessionID) {
				continue
			}
			if publicClient == nil || client.ConnectedAt.After(publicClient.ConnectedAt) {
				publicClient = client
			}
		}
		if publicClient != nil {
			preferred = publicClient
		}
	}

	if preferred != nil {
		addTarget(preferred)
		for key, other := range s.wsClients {
			if key == sessionID {
				continue
			}
			if other == preferred || (other != nil && other.sessionID == sessionID) {
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

func (s *Server) trunkUsername(trunkID int64) string {
	if s.trunkManager == nil || trunkID <= 0 {
		return ""
	}
	raw, ok := s.trunkManager.GetTrunkByID(trunkID)
	if !ok {
		return ""
	}
	trunk, ok := raw.(*sip.Trunk)
	if !ok || trunk == nil {
		return ""
	}
	return strings.TrimSpace(trunk.Username)
}

// selectOutOfDialogSIPMessageTargets fans out SIP MESSAGE with no matching call
// session to resolved WebSocket clients whose trunk username matches To.
func (s *Server) selectOutOfDialogSIPMessageTargets(to string) (targets []*WSClient, droppedDuplicate int) {
	targetUser := sipURIUsername(to)
	if targetUser == "" {
		return nil, 0
	}

	type resolvedClient struct {
		client  *WSClient
		trunkID int64
	}

	s.mu.RLock()
	candidates := make([]resolvedClient, 0)
	for client := range s.wsConnections {
		if client == nil || !client.trunkResolved || client.resolvedTrunkID <= 0 {
			continue
		}
		candidates = append(candidates, resolvedClient{client: client, trunkID: client.resolvedTrunkID})
	}
	s.mu.RUnlock()

	seen := make(map[*WSClient]struct{})
	usernameByTrunk := make(map[int64]string)
	for _, candidate := range candidates {
		username, cached := usernameByTrunk[candidate.trunkID]
		if !cached {
			username = s.trunkUsername(candidate.trunkID)
			usernameByTrunk[candidate.trunkID] = username
		}
		if !sipAddressMatches(to, targetUser, username) {
			continue
		}
		if _, ok := seen[candidate.client]; ok {
			droppedDuplicate++
			continue
		}
		seen[candidate.client] = struct{}{}
		targets = append(targets, candidate.client)
	}
	return targets, droppedDuplicate
}

func wsClientMatchesSession(client *WSClient, sessionID string) bool {
	if client == nil || strings.TrimSpace(sessionID) == "" {
		return false
	}
	if client.ownedSessionIDs != nil {
		if _, ok := client.ownedSessionIDs[sessionID]; ok {
			return true
		}
	}
	return client.sessionID == sessionID
}

// NotifySIPMessage notifies the WebSocket client associated with an incoming SIP message.
func (s *Server) NotifySIPMessage(to, from, body, contentType string) {
	s.NotifySIPMessageOnDialog("", to, from, body, contentType)
}

// NotifySIPMessageOnDialog prefers the SIP dialog Call-ID session (in-dialog
// Linphone/Asterisk MESSAGE) and falls back to To-header matching.
func (s *Server) NotifySIPMessageOnDialog(sipCallID, to, from, body, contentType string) {
	sessionID := ""
	if sipCallID != "" && s.sessionMgr != nil {
		if sess, ok := s.sessionMgr.GetSessionBySIPCallID(sipCallID); ok && sess != nil && sess.GetState() != session.StateEnded {
			sessionID = sess.ID
		}
	}
	if sessionID == "" {
		sessionID = s.findSIPMessageSessionID(to)
	}
	targets, totalConnections, droppedDuplicate := s.selectSIPMessageTargets(sessionID)
	if len(targets) == 0 && sessionID == "" {
		fallback, extraDup := s.selectOutOfDialogSIPMessageTargets(to)
		targets = fallback
		droppedDuplicate += extraDup
	}

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

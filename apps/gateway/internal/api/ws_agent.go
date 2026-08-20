package api

import (
	"context"
	"log"
	"strings"

	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/sip"
)

func (s *Server) handleWSAgentRegister(client *WSClient, msg WSMessage) {
	if client == nil || !client.agentOnly {
		s.sendWSError(client, msg.SessionID, "agent_register requires /ws-agent")
		return
	}
	if s.trunkManager == nil {
		s.sendWSError(client, msg.SessionID, "Trunk manager not available")
		return
	}

	domain := strings.TrimSpace(msg.SIPDomain)
	username := strings.TrimSpace(msg.SIPUsername)
	password := strings.TrimSpace(msg.SIPPassword)
	port := msg.SIPPort
	if domain == "" || username == "" || password == "" {
		s.sendWSError(client, msg.SessionID, "sipDomain, sipUsername, and sipPassword are required")
		return
	}
	// Capability negotiation is explicit so older ws-agent clients retain the
	// original one-call admission behavior.
	s.mu.Lock()
	client.multiCall = msg.MultiCall
	s.mu.Unlock()

	ctx := context.Background()
	trunk, err := s.trunkManager.UpsertAgentTrunk(ctx, sip.AgentTrunkPayload{
		Domain:   domain,
		Username: username,
		Password: password,
		Port:     port,
	})
	if err != nil {
		log.Printf("Agent register upsert failed: clientID=%s err=%v", client.clientID, err)
		s.sendWSError(client, msg.SessionID, "Failed to register agent trunk")
		return
	}
	if trunk == nil || trunk.ID <= 0 {
		s.sendWSError(client, msg.SessionID, "Failed to register agent trunk")
		return
	}

	needRegister, oldTrunkID, oldRemaining, err := s.bindAgentClient(client, trunk.ID)
	if err != nil {
		log.Printf("Agent register bind failed: clientID=%s trunkID=%d err=%v", client.clientID, trunk.ID, err)
		s.sendWSError(client, msg.SessionID, "Failed to bind agent trunk")
		return
	}
	if oldTrunkID > 0 && oldRemaining == 0 && oldTrunkID != trunk.ID {
		s.hangupSessionsForTrunk(oldTrunkID, "agent_rebind")
		if err := s.trunkManager.UnregisterTrunk(oldTrunkID, true); err != nil {
			log.Printf("Agent rebind unregister old trunk failed: trunkID=%d err=%v", oldTrunkID, err)
		}
	}
	if needRegister {
		if err := s.trunkManager.RegisterTrunk(trunk.ID, true); err != nil {
			log.Printf("Agent register SIP REGISTER failed: clientID=%s trunkID=%d err=%v", client.clientID, trunk.ID, err)
			s.unbindAgentClient(client, false)
			s.sendWSError(client, msg.SessionID, "Failed to SIP REGISTER agent trunk")
			return
		}
	}

	s.notifyWSClientChanged("updated", client)
	s.sendWSMessage(client, WSMessage{
		Type:          "trunk_resolved",
		TrunkID:       trunk.ID,
		TrunkPublicID: trunk.PublicID,
	})
	log.Printf("Agent register succeeded: clientID=%s trunkID=%d needRegister=%v", client.clientID, trunk.ID, needRegister)
}

// bindAgentClient attaches an agent WS client to a trunk and returns whether SIP REGISTER is required.
// If the client was bound to a different trunk, oldTrunkID/oldRemaining describe the previous binding after removal.
func (s *Server) bindAgentClient(client *WSClient, trunkID int64) (needRegister bool, oldTrunkID int64, oldRemaining int, err error) {
	if client == nil || trunkID <= 0 {
		return false, 0, 0, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.agentTrunkBindings == nil {
		s.agentTrunkBindings = make(map[int64]map[*WSClient]struct{})
	}

	if client.agentOnly && client.resolvedTrunkID > 0 && client.resolvedTrunkID != trunkID {
		oldTrunkID = client.resolvedTrunkID
		if set, ok := s.agentTrunkBindings[oldTrunkID]; ok {
			delete(set, client)
			if len(set) == 0 {
				delete(s.agentTrunkBindings, oldTrunkID)
				oldRemaining = 0
			} else {
				oldRemaining = len(set)
			}
		}
	}

	set := s.agentTrunkBindings[trunkID]
	if set == nil {
		set = make(map[*WSClient]struct{})
		s.agentTrunkBindings[trunkID] = set
	}
	_, alreadyBound := set[client]
	needRegister = !alreadyBound && len(set) == 0
	set[client] = struct{}{}
	client.trunkResolved = true
	client.resolvedTrunkID = trunkID
	return needRegister, oldTrunkID, oldRemaining, nil
}

// unbindAgentClient removes an agent client from trunk refcount. If unregisterOnLast
// is true and refcount hits zero, the caller should unregister (this helper only unbinds).
func (s *Server) unbindAgentClient(client *WSClient, _ bool) (trunkID int64, remaining int, wasBound bool) {
	if client == nil {
		return 0, 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	trunkID = client.resolvedTrunkID
	if trunkID > 0 && s.agentTrunkBindings != nil {
		if set, ok := s.agentTrunkBindings[trunkID]; ok {
			if _, exists := set[client]; exists {
				delete(set, client)
				wasBound = true
				if len(set) == 0 {
					delete(s.agentTrunkBindings, trunkID)
					remaining = 0
				} else {
					remaining = len(set)
				}
			}
		}
	}
	client.trunkResolved = false
	client.resolvedTrunkID = 0
	return trunkID, remaining, wasBound
}

func (s *Server) agentTrunkRefCount(trunkID int64) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.agentTrunkBindings == nil {
		return 0
	}
	return len(s.agentTrunkBindings[trunkID])
}

func (s *Server) cleanupAgentPresence(client *WSClient) {
	if client == nil || !client.agentOnly {
		return
	}

	// Snapshot and detach session ownership before any SIP network I/O. A
	// hanging BYE/CANCEL must not keep a closed agent in the routing maps.
	sessionIDs := s.ownedClientSessionIDs(client)
	for _, sessionID := range sessionIDs {
		s.unbindClientSession(client, sessionID)
	}

	// Remove the agent from the trunk refcount before ending calls. The trunk
	// may remain registered when another ws-agent connection still holds
	// presence; the last agent is handled below after call cleanup.
	trunkID, remaining, wasBound := s.unbindAgentClient(client, true)

	// Hang up every call owned by the disconnecting connection.
	for _, sessionID := range sessionIDs {
		if s.sessionMgr != nil {
			if sess, ok := s.sessionMgr.GetSession(sessionID); ok && sess != nil {
				s.forceEndSession(sess, "agent_disconnect")
			}
		}
	}

	if !wasBound || trunkID <= 0 {
		return
	}
	if remaining > 0 {
		log.Printf("Agent disconnect kept REGISTER: clientID=%s trunkID=%d remaining=%d", client.clientID, trunkID, remaining)
		return
	}

	// Last agent for this trunk: end any leftover sessions, then unregister immediately.
	s.hangupSessionsForTrunk(trunkID, "agent_last_disconnect")
	if s.trunkManager != nil {
		if err := s.trunkManager.UnregisterTrunk(trunkID, true); err != nil {
			log.Printf("Agent last-disconnect unregister failed: trunkID=%d err=%v", trunkID, err)
			return
		}
		log.Printf("Agent last-disconnect unregistered: trunkID=%d", trunkID)
	}
}

func (s *Server) hangupSessionsForTrunk(trunkID int64, reason string) {
	if s.sessionMgr == nil || trunkID <= 0 {
		return
	}
	for _, sess := range s.sessionMgr.ListSessions() {
		if sess == nil {
			continue
		}
		_, _, sessTrunkID, _, _, _, _ := sess.GetSIPAuthContext()
		if sessTrunkID != trunkID {
			continue
		}
		state := sess.GetState()
		if state == session.StateEnded {
			continue
		}
		s.forceEndSession(sess, reason)
	}
}

func (s *Server) forceEndSession(sess *session.Session, reason string) {
	if sess == nil {
		return
	}
	state := sess.GetState()
	if state == session.StateEnded {
		return
	}

	hasDialog := sess.HasDialogState()
	action := "end"
	switch {
	case state == session.StateIncoming:
		action = "reject"
	case hasDialog || state == session.StateActive:
		action = "bye"
	case state == session.StateConnecting || state == session.StateRinging:
		action = "cancel"
	}
	if !sess.TryBeginTerminalAction(action) {
		return
	}
	s.logTerminalAction(sess, action, 0, reason, "gateway")

	if s.sipMaker != nil {
		switch {
		case state == session.StateIncoming:
			_ = s.sipMaker.RejectCall(sess, "unavailable")
		case hasDialog || state == session.StateActive:
			_ = s.sipMaker.Hangup(sess)
		case state == session.StateConnecting || state == session.StateRinging:
			_ = s.sipMaker.CancelPendingCall(sess)
		default:
			sess.UpdateState(session.StateEnded)
		}
	} else {
		sess.UpdateState(session.StateEnded)
	}

	authMode, accountKey, trunkID, _, _, _, _ := sess.GetSIPAuthContext()
	if authMode == "public" && accountKey != "" && s.publicRegistry != nil {
		s.publicRegistry.DecrementRefCount(accountKey)
	}
	if authMode == "trunk" && trunkID > 0 && s.trunkManager != nil {
		_ = s.trunkManager.SetTrunkInUseBy(context.Background(), trunkID, nil)
	}
	s.sessionMgr.DeleteSession(sess.ID)
}

package api

import (
	"context"
	"log"
	"strings"
	"sync"

	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/sip"
)

type agentTrunkOwnership interface {
	IsTrunkOwned(trunkID int64) bool
}

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
		s.cleanupUnusedAgentTrunk(oldTrunkID, "agent_rebind")
	}
	// Serialize SIP registration changes for this trunk without holding the
	// shared server mutex. Presence can still change while network I/O is in
	// flight; cleanup revalidates it before deciding the final operation.
	unlockTrunkOperation := s.lockAgentTrunkOperation(trunk.ID)
	needRegister = needRegister || !s.agentTrunkOwned(trunk.ID)
	var registerErr error
	if needRegister {
		registerErr = s.trunkManager.RegisterTrunk(trunk.ID, true)
	}
	unlockTrunkOperation()
	if registerErr != nil {
		log.Printf("Agent register SIP REGISTER failed: clientID=%s trunkID=%d err=%v", client.clientID, trunk.ID, registerErr)
		s.unbindAgentClient(client, false)
		s.sendWSError(client, msg.SessionID, "Failed to SIP REGISTER agent trunk")
		return
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

func (s *Server) agentTrunkOwned(trunkID int64) bool {
	if s.trunkManager == nil || trunkID <= 0 {
		return false
	}
	if ownership, ok := s.trunkManager.(agentTrunkOwnership); ok {
		return ownership.IsTrunkOwned(trunkID)
	}
	for _, trunk := range s.trunkManager.ListOwnedTrunks() {
		if trunk != nil && trunk.ID == trunkID {
			return true
		}
	}
	return false
}

func (s *Server) lockAgentTrunkOperation(trunkID int64) func() {
	value, _ := s.agentTrunkOpLocks.LoadOrStore(trunkID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
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

	// The refcount snapshot can become stale while owned-session BYE/CANCEL is
	// in flight. Revalidate and compensate inside the trunk cleanup helper.
	s.cleanupUnusedAgentTrunk(trunkID, "agent_last_disconnect")
}

func (s *Server) cleanupUnusedAgentTrunk(trunkID int64, reason string) {
	if s.trunkManager == nil || trunkID <= 0 {
		return
	}
	unlockTrunkOperation := s.lockAgentTrunkOperation(trunkID)
	defer unlockTrunkOperation()
	if remaining := s.agentTrunkRefCount(trunkID); remaining > 0 {
		log.Printf("Agent stale cleanup skipped: trunkID=%d reason=%s remaining=%d", trunkID, reason, remaining)
		return
	}

	// Snapshot before network I/O. Sessions created by a replacement agent
	// after this point must never be swept into cleanup from the old socket.
	sessionIDs := s.sessionIDsForTrunk(trunkID)
	if remaining := s.agentTrunkRefCount(trunkID); remaining > 0 {
		log.Printf("Agent stale cleanup skipped after session snapshot: trunkID=%d reason=%s remaining=%d", trunkID, reason, remaining)
		return
	}
	for _, sessionID := range sessionIDs {
		if sess, ok := s.sessionMgr.GetSession(sessionID); ok && sess != nil {
			s.forceEndSession(sess, reason)
		}
	}

	// A replacement may have bound while orphan sessions were ending.
	if remaining := s.agentTrunkRefCount(trunkID); remaining > 0 {
		log.Printf("Agent stale unregister skipped: trunkID=%d reason=%s remaining=%d", trunkID, reason, remaining)
		return
	}
	if err := s.trunkManager.UnregisterTrunk(trunkID, true); err != nil {
		log.Printf("Agent unregister failed: trunkID=%d reason=%s err=%v", trunkID, reason, err)
		return
	}
	log.Printf("Agent trunk unregistered: trunkID=%d reason=%s", trunkID, reason)

	// If presence reappeared after the final refcount check, its REGISTER may
	// have completed before this stale UNREGISTER. Repair once more after the
	// unregister returns so the last completed operation matches live presence.
	if remaining := s.agentTrunkRefCount(trunkID); remaining > 0 {
		log.Printf("Agent presence reappeared during unregister; repairing REGISTER: trunkID=%d remaining=%d", trunkID, remaining)
		if err := s.trunkManager.RegisterTrunk(trunkID, true); err != nil {
			log.Printf("Agent REGISTER repair failed: trunkID=%d remaining=%d err=%v", trunkID, remaining, err)
			return
		}
		log.Printf("Agent REGISTER repair succeeded: trunkID=%d remaining=%d", trunkID, remaining)
	}
}

func (s *Server) sessionIDsForTrunk(trunkID int64) []string {
	if s.sessionMgr == nil || trunkID <= 0 {
		return nil
	}
	var sessionIDs []string
	for _, sess := range s.sessionMgr.ListSessions() {
		if sess == nil || sess.GetState() == session.StateEnded {
			continue
		}
		_, _, sessTrunkID, _, _, _, _ := sess.GetSIPAuthContext()
		if sessTrunkID == trunkID {
			sessionIDs = append(sessionIDs, sess.ID)
		}
	}
	return sessionIDs
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

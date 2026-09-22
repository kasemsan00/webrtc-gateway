package api

import (
	"context"
	"log"
	"strings"

	"webrtc-sip-gateway/internal/sip"
)

func (s *Server) handleWSDeviceRegister(client *WSClient, msg WSMessage) {
	if client == nil || !client.agentDeviceOnly {
		s.sendWSError(client, msg.SessionID, "device_register requires /ws-agent-device")
		return
	}
	if client.authClaims == nil || strings.TrimSpace(client.authClaims.Subject) == "" {
		s.sendWSError(client, msg.SessionID, "Authenticated subject is required")
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

	s.mu.Lock()
	client.multiCall = msg.MultiCall
	s.mu.Unlock()

	ctx := context.Background()
	trunk, err := s.trunkManager.UpsertAgentDeviceTrunk(ctx, sip.AgentTrunkPayload{
		Domain:   domain,
		Username: username,
		Password: password,
		Port:     port,
	})
	if err != nil {
		log.Printf("Agent-device register upsert failed: clientID=%s err=%v", client.clientID, err)
		s.sendWSError(client, msg.SessionID, "Failed to register agent-device trunk")
		return
	}
	if trunk == nil || trunk.ID <= 0 {
		s.sendWSError(client, msg.SessionID, "Failed to register agent-device trunk")
		return
	}

	subject := strings.TrimSpace(client.authClaims.Subject)
	platform := strings.TrimSpace(client.devicePlatform)
	if msgPlatform, ok := normalizeDevicePlatform(msg.DevicePlatform); ok && msgPlatform != "" {
		platform = msgPlatform
		client.devicePlatform = msgPlatform
	}
	var platformPtr *string
	if platform != "" {
		platformPtr = &platform
	}
	if err := s.trunkManager.SetTrunkNotifyUserIDAndPlatform(ctx, trunk.ID, &subject, platformPtr); err != nil {
		log.Printf("Agent-device notify bind failed: clientID=%s trunkID=%d err=%v", client.clientID, trunk.ID, err)
		s.sendWSError(client, msg.SessionID, "Failed to bind agent-device trunk")
		return
	}

	s.releaseStaleAgentDeviceTrunks(ctx, subject, trunk.ID)

	needRegister, oldTrunkID, oldRemaining, err := s.bindAgentClient(client, trunk.ID)
	if err != nil {
		log.Printf("Agent-device register bind failed: clientID=%s trunkID=%d err=%v", client.clientID, trunk.ID, err)
		s.sendWSError(client, msg.SessionID, "Failed to bind agent-device trunk")
		return
	}
	if oldTrunkID > 0 && oldRemaining == 0 && oldTrunkID != trunk.ID {
		s.releaseAgentDeviceTrunk(ctx, oldTrunkID, "agent_device_rebind")
	}

	unlockTrunkOperation := s.lockAgentTrunkOperation(trunk.ID)
	needRegister = needRegister || !s.agentTrunkOwned(trunk.ID)
	var registerErr error
	if needRegister {
		registerErr = s.trunkManager.RegisterTrunk(trunk.ID, true)
	}
	unlockTrunkOperation()
	if registerErr != nil {
		log.Printf("Agent-device SIP REGISTER failed: clientID=%s trunkID=%d err=%v", client.clientID, trunk.ID, registerErr)
		s.unbindAgentClient(client, false)
		s.sendWSError(client, msg.SessionID, "Failed to SIP REGISTER agent-device trunk")
		return
	}

	s.notifyWSClientChanged("updated", client)
	s.sendWSMessage(client, WSMessage{
		Type:          "trunk_resolved",
		TrunkID:       trunk.ID,
		TrunkPublicID: trunk.PublicID,
	})
	s.notifyPendingIncomingForClient(client, trunk.ID)
	log.Printf("Agent-device register succeeded: clientID=%s trunkID=%d needRegister=%v", client.clientID, trunk.ID, needRegister)
}

func (s *Server) handleWSDevicePushToken(client *WSClient, msg WSMessage) {
	if client == nil || !client.agentDeviceOnly {
		s.sendWSError(client, msg.SessionID, "device_push_token requires /ws-agent-device")
		return
	}
	if s.trunkManager == nil {
		s.sendWSError(client, msg.SessionID, "Trunk manager not available")
		return
	}
	if !client.trunkResolved || client.resolvedTrunkID <= 0 {
		s.sendWSError(client, msg.SessionID, "device_register is required before device_push_token")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(msg.PNType), "fcm") {
		s.sendWSError(client, msg.SessionID, "pnType must be fcm")
		return
	}
	token := strings.TrimSpace(msg.PNToken)
	if token == "" {
		s.sendWSError(client, msg.SessionID, "pnToken is required")
		return
	}
	if platform, ok := normalizeDevicePlatform(msg.DevicePlatform); ok && platform != "" {
		client.devicePlatform = platform
		subject := ""
		if client.authClaims != nil {
			subject = strings.TrimSpace(client.authClaims.Subject)
		}
		if subject != "" {
			if err := s.trunkManager.SetTrunkNotifyUserIDAndPlatform(context.Background(), client.resolvedTrunkID, &subject, &platform); err != nil {
				log.Printf("Agent-device platform update failed: clientID=%s trunkID=%d err=%v", client.clientID, client.resolvedTrunkID, err)
			}
		}
	}
	if err := s.trunkManager.SetTrunkFcmToken(context.Background(), client.resolvedTrunkID, token); err != nil {
		log.Printf("Agent-device FCM persist failed: clientID=%s trunkID=%d err=%v", client.clientID, client.resolvedTrunkID, err)
		s.sendWSError(client, msg.SessionID, "Failed to store device push token")
		return
	}
	log.Printf("Agent-device FCM token stored: clientID=%s trunkID=%d", client.clientID, client.resolvedTrunkID)
}

func (s *Server) handleWSDeviceUnregister(client *WSClient, msg WSMessage) {
	if client == nil || !client.agentDeviceOnly {
		s.sendWSError(client, msg.SessionID, "unregister requires /ws-agent-device")
		return
	}
	if s.trunkManager == nil {
		s.sendWSError(client, msg.SessionID, "Trunk manager not available")
		return
	}

	sessionIDs := s.ownedClientSessionIDs(client)
	for _, sessionID := range sessionIDs {
		s.unbindClientSession(client, sessionID)
	}
	for _, sessionID := range sessionIDs {
		if s.sessionMgr != nil {
			if sess, ok := s.sessionMgr.GetSession(sessionID); ok && sess != nil {
				s.forceEndSession(sess, "agent_device_unregister")
			}
		}
	}

	trunkID, _, wasBound := s.unbindAgentClient(client, true)
	if !wasBound || trunkID <= 0 {
		s.sendWSMessage(client, WSMessage{Type: "unregistered"})
		return
	}
	s.releaseAgentDeviceTrunk(context.Background(), trunkID, "agent_device_logout")
	s.sendWSMessage(client, WSMessage{Type: "unregistered"})
}

func (s *Server) cleanupAgentDevicePresence(client *WSClient) {
	if client == nil || !client.agentDeviceOnly {
		return
	}

	sessionIDs := s.ownedClientSessionIDs(client)
	for _, sessionID := range sessionIDs {
		s.unbindClientSession(client, sessionID)
	}
	s.unbindAgentClient(client, false)
	for _, sessionID := range sessionIDs {
		if s.sessionMgr != nil {
			if sess, ok := s.sessionMgr.GetSession(sessionID); ok && sess != nil {
				s.forceEndSession(sess, "agent_device_disconnect")
			}
		}
	}
}

func (s *Server) releaseStaleAgentDeviceTrunks(ctx context.Context, subject string, keepTrunkID int64) {
	if s.trunkManager == nil || strings.TrimSpace(subject) == "" {
		return
	}
	oldTrunks, err := s.trunkManager.ListAgentDeviceTrunksByNotifyUserID(ctx, subject)
	if err != nil {
		log.Printf("Agent-device stale trunk list failed: subject=%s err=%v", subject, err)
		return
	}
	for _, old := range oldTrunks {
		if old == nil || old.ID <= 0 || old.ID == keepTrunkID {
			continue
		}
		s.releaseAgentDeviceTrunk(ctx, old.ID, "agent_device_identity_move")
	}
}

func (s *Server) releaseAgentDeviceTrunk(ctx context.Context, trunkID int64, reason string) {
	if s.trunkManager == nil || trunkID <= 0 {
		return
	}
	unlockTrunkOperation := s.lockAgentTrunkOperation(trunkID)
	defer unlockTrunkOperation()
	if err := s.trunkManager.ClearTrunkFcmToken(ctx, trunkID); err != nil {
		log.Printf("Agent-device FCM clear failed: trunkID=%d reason=%s err=%v", trunkID, reason, err)
	}
	if err := s.trunkManager.SetTrunkNotifyUserID(ctx, trunkID, nil); err != nil {
		log.Printf("Agent-device notify clear failed: trunkID=%d reason=%s err=%v", trunkID, reason, err)
	}
	if err := s.trunkManager.UnregisterTrunk(trunkID, true); err != nil {
		log.Printf("Agent-device unregister failed: trunkID=%d reason=%s err=%v", trunkID, reason, err)
		return
	}
	log.Printf("Agent-device trunk unregistered: trunkID=%d reason=%s", trunkID, reason)
}

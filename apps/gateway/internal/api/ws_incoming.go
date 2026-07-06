package api

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
	"k2-gateway/internal/sip"
)

const (
	// Timeout for loading trunk notify_user_id from DB before dispatching push.
	incomingPushTrunkLookupTimeout = 5 * time.Second

	defaultIncomingRingTimeout = 30 * time.Second
)

const (
	clientAvailabilityIdle        = "idle"
	clientAvailabilityBusy        = "busy"
	clientAvailabilityUnavailable = "unavailable"
	incomingOfflinePolicyPush480  = "push_then_480"
)

const (
	devicePlatformAndroid = "android"
	devicePlatformIOS     = "ios"
)

type incomingPushRoute struct {
	SendFCM                 bool
	SendAPNS                bool
	UnknownPlatformFallback bool
	Reason                  string
}

func normalizeClientAvailability(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case clientAvailabilityBusy:
		return clientAvailabilityBusy
	case clientAvailabilityUnavailable:
		return clientAvailabilityUnavailable
	default:
		return clientAvailabilityIdle
	}
}

func normalizeClientCallState(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return string(session.StateNew)
	}
	return value
}

func isBusyCallState(value string) bool {
	switch normalizeClientCallState(value) {
	case string(session.StateConnecting), string(session.StateRinging), string(session.StateActive), string(session.StateIncoming), "calling", "incall":
		return true
	default:
		return false
	}
}

func isClientAvailableForIncoming(client *WSClient) bool {
	if client == nil {
		return false
	}
	if normalizeClientAvailability(client.availability) != clientAvailabilityIdle {
		return false
	}
	return !isBusyCallState(client.callState)
}

func (s *Server) incomingRingTimeout() time.Duration {
	if s.config.IncomingRingTimeoutSeconds <= 0 {
		return defaultIncomingRingTimeout
	}
	return time.Duration(s.config.IncomingRingTimeoutSeconds) * time.Second
}

func (s *Server) incrementIncomingCounter(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.incomingCounters == nil {
		s.incomingCounters = make(map[string]int64)
	}
	s.incomingCounters[name]++
}

func (s *Server) rejectIncomingSession(sessionID, statusReason, source string) {
	if s.sessionMgr == nil {
		return
	}
	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok || sess == nil {
		return
	}
	if sess.GetState() != session.StateIncoming && sess.GetState() != session.StateNew {
		return
	}
	if !sess.TryBeginTerminalAction(source) {
		log.Printf("📲 Incoming terminal action already claimed: sessionID=%s action=%s winner=%s", sessionID, source, sess.GetTerminalAction())
		return
	}

	statusCode := 486
	if statusReason == "no_answer" || statusReason == "unavailable" || statusReason == "offline" {
		statusCode = 480
	}
	s.logTerminalAction(sess, source, statusCode, statusReason, "gateway")
	if s.sipMaker != nil {
		if err := s.sipMaker.RejectCall(sess, statusReason); err != nil {
			log.Printf("⚠️ Failed to reject incoming session %s reason=%s: %v", sessionID, statusReason, err)
		}
	} else {
		sess.UpdateState(session.StateEnded)
	}
	s.sessionMgr.DeleteSession(sessionID)
}

func (s *Server) hasIncomingPushTarget(trunkID int64) bool {
	if s.config.IncomingOfflinePolicy != "" && s.config.IncomingOfflinePolicy != incomingOfflinePolicyPush480 {
		return false
	}
	if s.pushService == nil || s.trunkManager == nil || trunkID <= 0 {
		return false
	}
	lookupCtx, cancel := context.WithTimeout(context.Background(), incomingPushTrunkLookupTimeout)
	defer cancel()
	trunk, err := s.trunkManager.GetTrunkByIDFromDB(lookupCtx, trunkID)
	if err != nil || trunk == nil {
		return false
	}
	route := selectIncomingPushRoute(trunk, s.pushService.CanSendAPNS(), s.config.TrunkPNAppID)
	return route.SendFCM || route.SendAPNS
}

func trunkHasFCMPushTarget(trunk *sip.Trunk) bool {
	return trunk != nil && trunk.NotifyUserID != nil && strings.TrimSpace(*trunk.NotifyUserID) != ""
}

func trunkHasApplePushKitTarget(trunk *sip.Trunk, expectedAppID string) bool {
	if trunk == nil || trunk.PNAppID == nil || trunk.PNType == nil || trunk.PNToken == nil {
		return false
	}
	return strings.TrimSpace(*trunk.PNAppID) == normalizeTrunkPNAppID(expectedAppID) &&
		strings.EqualFold(strings.TrimSpace(*trunk.PNType), trunkPNType) &&
		strings.TrimSpace(*trunk.PNToken) != ""
}

func normalizeDevicePlatform(platform string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(platform))
	if value == "" || value == devicePlatformAndroid || value == devicePlatformIOS {
		return value, true
	}
	return "", false
}

func selectIncomingPushRoute(trunk *sip.Trunk, canSendAPNS bool, expectedAppID string) incomingPushRoute {
	hasFCM := trunkHasFCMPushTarget(trunk)
	hasAPNS := canSendAPNS && trunkHasApplePushKitTarget(trunk, expectedAppID)
	if trunk == nil || (!hasFCM && !hasAPNS) {
		return incomingPushRoute{Reason: "missing_push_target"}
	}

	platform := ""
	if trunk.LastOnlinePlatform != nil {
		platform, _ = normalizeDevicePlatform(*trunk.LastOnlinePlatform)
	}

	switch platform {
	case devicePlatformAndroid:
		if hasFCM {
			return incomingPushRoute{SendFCM: true, Reason: "android_fcm"}
		}
		return incomingPushRoute{Reason: "android_missing_fcm_target"}
	case devicePlatformIOS:
		if hasAPNS {
			return incomingPushRoute{SendAPNS: true, Reason: "ios_apns"}
		}
		return incomingPushRoute{Reason: "ios_missing_apns_target"}
	default:
		return incomingPushRoute{
			SendFCM:                 hasFCM,
			SendAPNS:                hasAPNS,
			UnknownPlatformFallback: true,
			Reason:                  "unknown_platform_fallback",
		}
	}
}

func (s *Server) incomingSessionHasVideo(sessionID string) bool {
	if s.sessionMgr == nil {
		return false
	}
	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok || sess == nil {
		return false
	}
	_, _, inviteBody, _, _ := sess.GetIncomingInvite()
	return hasActiveVideoMedia(string(inviteBody))
}

func (s *Server) dispatchIncomingPush(sessionID, from, to string, trunkID int64, hasVideo bool) {
	if s.pushService == nil || s.trunkManager == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("🔔 [Push] Incoming call push panic recovered: sessionID=%s trunkID=%d panic=%v", sessionID, trunkID, r)
			}
		}()
		lookupCtx, cancel := context.WithTimeout(context.Background(), incomingPushTrunkLookupTimeout)
		defer cancel()

		trunk, err := s.trunkManager.GetTrunkByIDFromDB(lookupCtx, trunkID)
		if err != nil {
			log.Printf("🔔 [Push] Skip incoming call push: failed to load trunk from DB (sessionID=%s trunkID=%d err=%v)", sessionID, trunkID, err)
			return
		}
		dispatched := false
		route := selectIncomingPushRoute(trunk, s.pushService.CanSendAPNS(), s.config.TrunkPNAppID)
		if route.UnknownPlatformFallback {
			log.Printf("🔔 [Push] Incoming push using unknown platform fallback: sessionID=%s trunkID=%d", sessionID, trunkID)
		}
		if route.SendAPNS {
			s.pushService.NotifyIncomingCallAPNS(*trunk.PNToken, sessionID, from, to, hasVideo)
			dispatched = true
		}
		if route.SendFCM {
			log.Printf("🔔 [Push] Dispatch incoming call FCM fallback: userID=%s sessionID=%s trunkID=%d", *trunk.NotifyUserID, sessionID, trunkID)
			s.pushService.NotifyIncomingCall(*trunk.NotifyUserID, sessionID, from, to, hasVideo)
			dispatched = true
		}
		if !dispatched {
			log.Printf("🔔 [Push] Skip incoming call push: route=%s sessionID=%s trunkID=%d", route.Reason, sessionID, trunkID)
		}
	}()
}

func (s *Server) startIncomingRingTimeout(sessionID string, trunkID int64) {
	timeout := s.incomingRingTimeout()
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		<-timer.C

		if s.sessionMgr == nil {
			return
		}
		sess, ok := s.sessionMgr.GetSession(sessionID)
		if !ok || sess == nil || sess.GetState() != session.StateIncoming {
			return
		}
		if !sess.TryBeginTerminalAction("timeout") {
			return
		}
		log.Printf("⏱️ Incoming call timed out: sessionID=%s trunkID=%d timeout=%s", sessionID, trunkID, timeout)
		s.incrementIncomingCounter("incoming_no_answer")
		s.logTerminalAction(sess, "timeout", 480, "no_answer", "gateway")
		if trunkID > 0 {
			s.NotifyIncomingCancel(sessionID, trunkID, "no_answer")
		}
		if s.sipMaker != nil {
			if err := s.sipMaker.RejectCall(sess, "no_answer"); err != nil {
				log.Printf("⚠️ Failed to reject incoming timeout session %s: %v", sessionID, err)
			}
		} else {
			sess.UpdateState(session.StateEnded)
		}
		s.sessionMgr.DeleteSession(sessionID)
	}()
}

// NotifyIncomingCall notifies eligible WebSocket clients about an incoming call for a specific trunk.
func (s *Server) NotifyIncomingCall(sessionID, from, to string, trunkID int64) {
	if trunkID <= 0 {
		log.Printf("📲 Skipping incoming call notification for session %s: missing trunkID", sessionID)
		return
	}
	hasVideo := s.incomingSessionHasVideo(sessionID)
	hasVideoValue := strconv.FormatBool(hasVideo)

	s.mu.RLock()
	totalConnections := len(s.wsConnections)
	matchingClients := 0
	busyClients := 0
	unavailableClients := 0
	idleClients := make([]*WSClient, 0)
	recipientSessionIDs := make([]string, 0)

	for client := range s.wsConnections {
		if client == nil || !client.trunkResolved || client.resolvedTrunkID != trunkID {
			continue
		}
		matchingClients++
		if isClientAvailableForIncoming(client) {
			idleClients = append(idleClients, client)
			continue
		}
		if normalizeClientAvailability(client.availability) == clientAvailabilityUnavailable {
			unavailableClients++
		} else {
			busyClients++
		}
	}
	s.mu.RUnlock()

	if len(idleClients) > 0 {
		for _, client := range idleClients {
			recipientSessionIDs = append(recipientSessionIDs, client.sessionID)
			s.sendWSMessage(client, WSMessage{
				Type:      "incoming",
				SessionID: sessionID,
				From:      from,
				To:        to,
				HasVideo:  hasVideoValue,
			})
			log.Printf("📲 Sent incoming call notification to resolved client (sessionID=%s trunkID=%d)", sessionID, trunkID)
		}
		s.incrementIncomingCounter("incoming_presented")
		if s.hasIncomingPushTarget(trunkID) {
			s.incrementIncomingCounter("incoming_push_wait")
			s.dispatchIncomingPush(sessionID, from, to, trunkID, hasVideo)
		}
		s.startIncomingRingTimeout(sessionID, trunkID)
		log.Printf("📲 Incoming fanout summary: sessionID=%s trunkID=%d recipients=%d recipientSessionIDs=%v filtered=%d total=%d", sessionID, trunkID, len(idleClients), recipientSessionIDs, totalConnections-len(idleClients), totalConnections)
		return
	}

	if matchingClients > 0 {
		reason := "busy"
		counter := "incoming_busy"
		if busyClients == 0 && unavailableClients > 0 {
			reason = "unavailable"
			counter = "incoming_unavailable"
		}
		s.incrementIncomingCounter(counter)
		log.Printf("📲 Incoming admission rejected: sessionID=%s trunkID=%d reason=%s matching=%d busy=%d unavailable=%d", sessionID, trunkID, reason, matchingClients, busyClients, unavailableClients)
		s.rejectIncomingSession(sessionID, reason, reason)
		return
	}

	if totalConnections == 0 {
		log.Printf("⚠️ No WebSocket clients connected for incoming call notification")
	}

	if s.hasIncomingPushTarget(trunkID) {
		s.incrementIncomingCounter("incoming_push_wait")
		s.dispatchIncomingPush(sessionID, from, to, trunkID, hasVideo)
		s.startIncomingRingTimeout(sessionID, trunkID)
		return
	}

	s.incrementIncomingCounter("incoming_offline")
	log.Printf("📲 Incoming admission rejected: sessionID=%s trunkID=%d reason=offline_no_push", sessionID, trunkID)
	s.rejectIncomingSession(sessionID, "offline", "offline")
}

// NotifyIncomingCancel notifies connected WebSocket clients that an incoming call was cancelled by caller.
func (s *Server) NotifyIncomingCancel(sessionID string, trunkID int64, reason string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if trunkID <= 0 {
		log.Printf("📲 Skipping incoming cancel notification for session %s: missing trunkID", sessionID)
		return
	}

	totalConnections := len(s.wsConnections)
	recipients := 0
	recipientSessionIDs := make([]string, 0)

	for client := range s.wsConnections {
		if client == nil || !client.trunkResolved || client.resolvedTrunkID != trunkID {
			continue
		}
		recipients++
		recipientSessionIDs = append(recipientSessionIDs, client.sessionID)
		s.sendWSMessage(client, WSMessage{
			Type:      "cancel",
			SessionID: sessionID,
			Reason:    reason,
		})
		log.Printf("📲 Sent incoming cancel notification to resolved client (sessionID=%s trunkID=%d)", sessionID, trunkID)
	}

	if totalConnections == 0 {
		log.Printf("⚠️ No WebSocket clients connected for incoming cancel notification")
	} else {
		log.Printf("📲 Incoming cancel fanout summary: sessionID=%s trunkID=%d recipients=%d recipientSessionIDs=%v filtered=%d total=%d", sessionID, trunkID, recipients, recipientSessionIDs, totalConnections-recipients, totalConnections)
	}
}

// handleWSAccept handles WebSocket accept messages for incoming calls
func (s *Server) handleWSAccept(client *WSClient, msg WSMessage) {
	if msg.SessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}

	log.Printf("📞 [Accept] Incoming sessionID: %s, Client sessionID: %s", msg.SessionID, client.sessionID)

	// Get the incoming call session (this has the SIP transaction but no WebRTC)
	incomingSess, ok := s.sessionMgr.GetSession(msg.SessionID)
	if !ok {
		s.sendWSError(client, msg.SessionID, "Session not found")
		return
	}
	if incomingSess.GetState() != session.StateIncoming {
		s.sendWSMessage(client, WSMessage{Type: "state", SessionID: msg.SessionID, State: string(incomingSess.GetState())})
		return
	}

	// Find the client's existing WebRTC session (this has WebRTC but no SIP)
	var webrtcSess *session.Session
	webrtcSessionFound := false
	webrtcPeerConnectionReady := false
	if client.sessionID != "" && client.sessionID != msg.SessionID {
		if sess, ok := s.sessionMgr.GetSession(client.sessionID); ok {
			webrtcSessionFound = true
			if sess.PeerConnection != nil {
				webrtcSess = sess
				webrtcPeerConnectionReady = true
				log.Printf("📞 Found client's WebRTC session: %s", webrtcSess.ID)
			} else {
				log.Printf("⚠️ Client session %s has no PeerConnection", client.sessionID)
			}
		} else {
			log.Printf("⚠️ Client session %s not found in sessionMgr", client.sessionID)
		}
	} else {
		log.Printf("⚠️ No valid client.sessionID (empty=%v, same=%v)", client.sessionID == "", client.sessionID == msg.SessionID)
	}

	log.Printf("📈 [Accept] decision incomingSessionID=%s clientSessionID=%s webrtcSessionFound=%v webrtcPeerConnectionReady=%v willTransferSIP=%v",
		msg.SessionID, client.sessionID, webrtcSessionFound, webrtcPeerConnectionReady, webrtcSess != nil)

	if webrtcSess == nil {
		log.Printf("⚠️ [Accept] Rejecting accept without ready WebRTC session: incomingSessionID=%s clientSessionID=%s", msg.SessionID, client.sessionID)
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: incomingSess.ID,
			Category:  "ws",
			Name:      "ws_accept_without_webrtc_session",
			Data: map[string]interface{}{
				"incomingSessionId":         msg.SessionID,
				"clientSessionId":           client.sessionID,
				"webrtcSessionFound":        webrtcSessionFound,
				"webrtcPeerConnectionReady": webrtcPeerConnectionReady,
				"result":                    "rejected",
			},
		})
		s.sendWSError(client, msg.SessionID, "WebRTC session required before accepting incoming call")
		return
	}

	if !incomingSess.TryBeginTerminalAction("accept") {
		s.sendWSError(client, msg.SessionID, "Call already has a terminal action in progress")
		return
	}

	// First-accept-wins: Try to claim the incoming call
	clientID := fmt.Sprintf("%p", client) // Use client pointer as unique ID
	if !incomingSess.TryClaimIncoming(clientID) {
		// Already claimed by another client
		log.Printf("⚠️ [Accept] Session %s already claimed by another client", msg.SessionID)
		incomingSess.ClearTerminalAction()
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: incomingSess.ID,
			Category:  "ws",
			Name:      "ws_incoming_action_result",
			Data: map[string]interface{}{
				"incomingAction": "sending_accept",
				"result":         "already_claimed",
				"sessionId":      msg.SessionID,
			},
		})
		s.sendWSError(client, msg.SessionID, "Call already accepted by another client")
		return
	}

	log.Printf("✅ [Accept] Session %s claimed by client %s", msg.SessionID, clientID)

	ctx := context.Background()
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: incomingSess.ID,
		Category:  "ws",
		Name:      "ws_accept_request",
	})

	// If we have a WebRTC session, transfer SIP data to it
	log.Printf("📞 Transferring SIP data from session %s to WebRTC session %s", incomingSess.ID, webrtcSess.ID)

	// Transfer SIP transaction and request to WebRTC session (thread-safe)
	webrtcSess.CopyIncomingInviteFrom(incomingSess)
	_, from, to, sipCallID := incomingSess.GetCallInfo()
	webrtcSess.SetCallInfo("inbound", from, to, sipCallID)

	// Determine which session to use for the call
	callSession := webrtcSess
	incomingSessionID := msg.SessionID // Remember for later deletion
	log.Printf("📈 [Accept] selected_call_session incomingSessionID=%s callSessionID=%s transferredSIP=true", incomingSessionID, callSession.ID)

	// Associate client with the call session
	if client.sessionID == "" || client.sessionID != callSession.ID {
		client.sessionID = callSession.ID
		s.mu.Lock()
		s.wsClients[callSession.ID] = client
		s.mu.Unlock()
	}

	// Accept the incoming call
	acceptError := false
	if s.sipMaker != nil {
		if err := s.sipMaker.AcceptCall(callSession); err != nil {
			log.Printf("⚠️ AcceptCall error (call may still work via retransmission): %v", err)
			// Don't return - the call might still work
			// The 200 OK might have been sent despite the error (e.g., "transaction terminated")
			// We'll still send the state update so browser knows the correct session ID
			acceptError = true
		}
	}

	s.logSessionSnapshot(ctx, callSession, "")

	// Delete the old incoming session (even if AcceptCall reported error, call may work)
	if webrtcSess != nil && incomingSessionID != webrtcSess.ID {
		log.Printf("🗑️ Deleting old incoming session: %s", incomingSessionID)
		s.sessionMgr.DeleteSession(incomingSessionID)
	}

	// ALWAYS send state update with the CORRECT session ID
	// This ensures browser uses the right session for hangup
	response := WSMessage{
		Type:      "state",
		SessionID: callSession.ID,
		State:     "active", // Assume active - dialog state will be set from ACK
	}
	s.sendWSMessage(client, response)

	if acceptError {
		log.Printf("⚠️ Call accepted with warning, using session: %s (dialog state will be set from ACK)", callSession.ID)
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: callSession.ID,
			Category:  "ws",
			Name:      "ws_incoming_action_result",
			Data: map[string]interface{}{
				"incomingAction": "sending_accept",
				"result":         "accepted_with_warning",
				"sessionId":      callSession.ID,
			},
		})
	} else {
		log.Printf("✅ Call accepted, using session: %s", callSession.ID)
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: callSession.ID,
			Category:  "ws",
			Name:      "ws_incoming_action_result",
			Data: map[string]interface{}{
				"incomingAction": "sending_accept",
				"result":         "accepted",
				"sessionId":      callSession.ID,
			},
		})
	}
	callSession.ClearTerminalAction()
	s.incrementIncomingCounter("incoming_accepted")
}

func isBenignIncomingRejectError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "transaction terminated")
}

// handleWSReject handles WebSocket reject messages for incoming calls
func (s *Server) handleWSReject(client *WSClient, msg WSMessage) {
	if msg.SessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(msg.SessionID)
	if !ok {
		s.sendWSError(client, msg.SessionID, "Session not found")
		return
	}
	if sess.GetState() != session.StateIncoming {
		s.sendWSMessage(client, WSMessage{Type: "state", SessionID: msg.SessionID, State: string(sess.GetState())})
		return
	}

	ctx := context.Background()
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "ws",
		Name:      "ws_reject_request",
		Data: map[string]interface{}{
			"incomingAction": "sending_reject",
			"reason":         msg.Reason,
			"reasonSource":   msg.ReasonSource,
		},
	})

	reason := msg.Reason
	if reason == "" {
		reason = "busy" // Default to busy
	}
	reasonSource := strings.TrimSpace(msg.ReasonSource)
	if reasonSource == "" {
		reasonSource = "client_unspecified"
	}
	log.Printf("📴 [Reject] Received incoming reject via WS (session=%s, reason=%s, source=%s)", msg.SessionID, reason, reasonSource)
	if !sess.TryBeginTerminalAction("reject") {
		log.Printf("📴 [Reject] Duplicate incoming reject ignored (session=%s winner=%s)", msg.SessionID, sess.GetTerminalAction())
		s.sendWSMessage(client, WSMessage{Type: "state", SessionID: msg.SessionID, State: string(session.StateEnded)})
		return
	}
	statusCode := 486
	if reason == "no_answer" || reason == "unavailable" || reason == "offline" {
		statusCode = 480
	}
	s.logTerminalAction(sess, "reject", statusCode, reason, "client:"+reasonSource)

	// Decrement public account refcount if applicable (before deleting session)
	authMode, accountKey, _, _, _, _, _ := sess.GetSIPAuthContext()
	if authMode == "public" && accountKey != "" && s.publicRegistry != nil {
		s.publicRegistry.DecrementRefCount(accountKey)
	}

	// Reject the incoming call
	if s.sipMaker != nil {
		if err := s.sipMaker.RejectCall(sess, reason); err != nil {
			if isBenignIncomingRejectError(err) {
				log.Printf("⚠️ [Reject] Incoming reject reached terminated transaction, treating as ended (session=%s): %v", msg.SessionID, err)
				s.logEvent(&logstore.Event{
					Timestamp: time.Now(),
					SessionID: sess.ID,
					Category:  "ws",
					Name:      "ws_incoming_action_result",
					Data: map[string]interface{}{
						"incomingAction": "sending_reject",
						"reason":         reason,
						"reasonSource":   reasonSource,
						"result":         "already_terminated",
						"sessionId":      msg.SessionID,
					},
				})
			} else {
				s.logEvent(&logstore.Event{
					Timestamp: time.Now(),
					SessionID: sess.ID,
					Category:  "ws",
					Name:      "ws_incoming_action_result",
					Data: map[string]interface{}{
						"incomingAction": "sending_reject",
						"reason":         reason,
						"reasonSource":   reasonSource,
						"result":         "failed",
						"sessionId":      msg.SessionID,
						"error":          err.Error(),
					},
				})
				s.sendWSError(client, msg.SessionID, fmt.Sprintf("Failed to reject call: %v", err))
				return
			}
		} else {
			log.Printf("✅ [Reject] SIP reject sent successfully (session=%s, reason=%s, source=%s)", msg.SessionID, reason, reasonSource)
			s.incrementIncomingCounter("incoming_rejected")
			s.logEvent(&logstore.Event{
				Timestamp: time.Now(),
				SessionID: sess.ID,
				Category:  "ws",
				Name:      "ws_incoming_action_result",
				Data: map[string]interface{}{
					"incomingAction": "sending_reject",
					"reason":         reason,
					"reasonSource":   reasonSource,
					"result":         "rejected",
					"sessionId":      msg.SessionID,
				},
			})
		}
	}

	// Ensure terminal state is reflected for clients even if SIP transaction already ended.
	sess.UpdateState(session.StateEnded)

	// Delete session
	s.sessionMgr.DeleteSession(msg.SessionID)
	s.logSessionSnapshot(ctx, sess, "ws_reject")

	// Send state update
	response := WSMessage{
		Type:      "state",
		SessionID: msg.SessionID,
		State:     string(session.StateEnded),
	}
	s.sendWSMessage(client, response)
}

// notifyPendingIncomingForClient replays queued incoming calls to a specific client.
// This is used after trunk_resolve so UI can pick up incoming calls that arrived before resolve.
func (s *Server) notifyPendingIncomingForClient(client *WSClient, trunkID int64) {
	if s.sessionMgr == nil || client == nil {
		return
	}

	for _, sess := range s.sessionMgr.ListSessions() {
		if sess == nil || sess.GetState() != session.StateIncoming {
			continue
		}
		authMode, _, sessTrunkID, _, _, _, _ := sess.GetSIPAuthContext()
		if authMode != "trunk" || sessTrunkID != trunkID {
			continue
		}
		if !isClientAvailableForIncoming(client) {
			continue
		}
		_, from, to, _ := sess.GetCallInfo()
		_, _, inviteBody, _, _ := sess.GetIncomingInvite()
		hasVideoValue := strconv.FormatBool(hasActiveVideoMedia(string(inviteBody)))
		s.sendWSMessage(client, WSMessage{
			Type:      "incoming",
			SessionID: sess.ID,
			From:      from,
			To:        to,
			HasVideo:  hasVideoValue,
		})
	}
}

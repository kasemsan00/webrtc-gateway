package sip

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"

	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
)

// setupHandlers configures SIP request handlers
func (s *Server) setupHandlers() {
	// Handle INVITE requests (incoming calls)
	s.sipServer.OnInvite(func(req *sip.Request, tx sip.ServerTransaction) {
		s.handleINVITE(req, tx)
	})

	// Handle BYE requests (call termination)
	s.sipServer.OnBye(func(req *sip.Request, tx sip.ServerTransaction) {
		s.handleBYE(req, tx)
	})

	// Handle ACK requests
	s.sipServer.OnAck(func(req *sip.Request, tx sip.ServerTransaction) {
		s.handleACK(req, tx)
	})

	// Handle OPTIONS requests (keep-alive / availability check)
	s.sipServer.OnOptions(func(req *sip.Request, tx sip.ServerTransaction) {
		s.handleOPTIONS(req, tx)
	})

	// Handle MESSAGE requests (instant messaging)
	s.sipServer.OnMessage(func(req *sip.Request, tx sip.ServerTransaction) {
		s.handleMESSAGE(req, tx)
	})
}

// handleINVITE handles incoming INVITE requests
func (s *Server) handleINVITE(req *sip.Request, tx sip.ServerTransaction) {
	ctx := context.Background()
	// Log incoming INVITE for debugging
	sourceIP := req.Source()
	activeDomain := s.getActiveDomain()
	fmt.Printf("\n📞 [DEBUG] Received INVITE from: %s (activeDomain: %s)\n", sourceIP, activeDomain)

	// Extract Call-ID and caller info
	var callIDValue string
	if callID := req.CallID(); callID != nil {
		callIDValue = callID.Value()
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: "",
		Category:  "sip",
		Name:      "sip_invite_received",
		SIPMethod: string(req.Method),
		SIPCallID: callIDValue,
		Data:      map[string]interface{}{"source": sourceIP},
	})

	if s.logFullSIP {
		_ = s.storePayload(ctx, &logstore.PayloadRecord{
			SessionID:   "",
			Timestamp:   time.Now(),
			Kind:        "sip_message",
			ContentType: "application/sip",
			BodyText:    req.String(),
		})
	}

	fromURI := ""
	fromDisplayName := ""
	if fromHeader := req.From(); fromHeader != nil {
		fromURI = fromHeader.Address.String()
		fromDisplayName = fromHeader.DisplayName
	}

	toURI := ""
	if toHeader := req.To(); toHeader != nil {
		toURI = toHeader.Address.String()
	}

	fmt.Printf("\n=== Inbound INVITE ===\n")
	fmt.Printf("From: %s (%s)\n", fromURI, fromDisplayName)
	fmt.Printf("To: %s\n", toURI)
	fmt.Printf("Call-ID: %s\n", callIDValue)

	// Debug: Print all headers to see if Record-Route is present
	if s.config.DebugSIPInvite {
		fmt.Printf("\n--- All INVITE Headers ---\n")
		fmt.Printf("%s\n", req.String())
		fmt.Printf("-------------------------\n\n")
	}

	// NEW: Check if this incoming call is for a trunk we own
	var isTrunkCall bool
	var trunkOwned bool
	var matchedTrunk *Trunk
	if s.trunkManager != nil {
		match := s.trunkManager.MatchTrunkFromInviteDetailed(req)
		if match.Trunk != nil {
			isTrunkCall = true
			trunkOwned = match.Owned
			matchedTrunk = match.Trunk
			fmt.Printf("📞 INVITE matched trunk ID %d (name: %s, owned: %v, rule=%s, sipUser=%q, candidates=%v, ownedCandidates=%v)\n",
				match.Trunk.ID, match.Trunk.Name, match.Owned, match.Rule, match.SIPUser, match.CandidateIDs, match.OwnedCandidates)

			if !match.Owned {
				// Trunk exists but not owned by this instance - reject with 404
				fmt.Printf("❌ Trunk not owned by this instance - rejecting INVITE\n")
				res := sip.NewResponseFromRequest(req, 404, "Not Found", nil)
				tx.Respond(res)
				s.logEvent(&logstore.Event{
					Timestamp:     time.Now(),
					SessionID:     "",
					Category:      "sip",
					Name:          "sip_invite_rejected_trunk_not_owned",
					SIPStatusCode: 404,
					SIPCallID:     callIDValue,
					Data: map[string]interface{}{
						"trunkId":         match.Trunk.ID,
						"trunkName":       match.Trunk.Name,
						"matchRule":       match.Rule,
						"sipUser":         match.SIPUser,
						"candidates":      match.CandidateIDs,
						"ownedCandidates": match.OwnedCandidates,
					},
				})
				return
			}
		} else if match.Ambiguous {
			fmt.Printf("❌ Ambiguous trunk match for INVITE - rejecting (rule=%s sipUser=%q candidates=%v ownedCandidates=%v)\n",
				match.Rule, match.SIPUser, match.CandidateIDs, match.OwnedCandidates)
			res := sip.NewResponseFromRequest(req, 503, "Service Unavailable", nil)
			tx.Respond(res)
			s.logEvent(&logstore.Event{
				Timestamp:     time.Now(),
				SessionID:     "",
				Category:      "sip",
				Name:          "sip_invite_rejected_ambiguous_trunk_match",
				SIPStatusCode: 503,
				SIPCallID:     callIDValue,
				Data: map[string]interface{}{
					"matchRule":       match.Rule,
					"sipUser":         match.SIPUser,
					"candidates":      match.CandidateIDs,
					"ownedCandidates": match.OwnedCandidates,
				},
			})
			return
		} else {
			// No trunk match - this is a SIP public incoming call attempt
			isTrunkCall = false
			fmt.Printf("⚠️ INVITE does not match any trunk - rejecting (rule=%s, sipUser=%q, ownedCandidates=%v, SIP public incoming not allowed)\n",
				match.Rule, match.SIPUser, match.OwnedCandidates)
			res := sip.NewResponseFromRequest(req, 403, "Forbidden", nil)
			tx.Respond(res)
			s.logEvent(&logstore.Event{
				Timestamp:     time.Now(),
				SessionID:     "",
				Category:      "sip",
				Name:          "sip_invite_rejected_no_trunk",
				SIPStatusCode: 403,
				SIPCallID:     callIDValue,
				Data: map[string]interface{}{
					"matchRule":       match.Rule,
					"sipUser":         match.SIPUser,
					"ownedCandidates": match.OwnedCandidates,
				},
			})
			return
		}
	}

	// Try to find existing session by Call-ID (for re-INVITEs or retransmissions)
	var sess *session.Session
	if s.sessionMgr != nil && callIDValue != "" {
		if existingSess, ok := s.sessionMgr.GetSessionBySIPCallID(callIDValue); ok {
			sess = existingSess
			fmt.Printf("📞 Found existing session: %s (re-INVITE/retransmission)\n", sess.ID)

			s.logEvent(&logstore.Event{
				Timestamp: time.Now(),
				SessionID: sess.ID,
				Category:  "sip",
				Name:      "sip_reinvite_received",
				SIPMethod: string(req.Method),
				SIPCallID: callIDValue,
			})

			// CRITICAL: Update the transaction - old one may have timed out
			sess.UpdateIncomingInviteTransaction(tx, req, req.Body())

			// If session is still in "incoming" state, resend 180 Ringing
			if sess.GetState() == session.StateIncoming {
				fmt.Printf("📞 Resending 180 Ringing for session %s\n", sess.ID)
				ringingRes := sip.NewResponseFromRequest(req, 180, "Ringing", nil)
				ringingRes.AppendHeader(&sip.ContactHeader{
					Address: sip.Uri{Host: s.publicAddress, Port: s.sipPort},
				})
				tx.Respond(ringingRes)
			}
			// If already active, this might be a mid-call re-INVITE (hold/resume)
			if sess.GetState() == session.StateActive {
				tryingRes := sip.NewResponseFromRequest(req, 100, "Trying", nil)
				tx.Respond(tryingRes)
			}
			return // Don't create new session
		}
	}

	// NEW: For trunk calls, only proceed if it's a trunk we own
	if isTrunkCall && trunkOwned {
		fmt.Printf("📞 Processing trunk incoming call\n")
	} else if !isTrunkCall {
		// Already rejected above, this code path shouldn't be reached
		fmt.Printf("❌ Non-trunk incoming call - already rejected\n")
		return
	}

	// NEW: For new incoming calls, create a session and notify browser
	if s.sessionCreator != nil && s.incomingNotifier != nil {
		fmt.Printf("📞 New incoming call - creating session and notifying browser\n")

		// Send 100 Trying
		tryingRes := sip.NewResponseFromRequest(req, 100, "Trying", nil)
		tx.Respond(tryingRes)

		// Create a new session for this incoming call
		newSess, err := s.sessionCreator.CreateSessionForIncoming(s.turnConfig)
		if err != nil {
			fmt.Printf("❌ Error creating session for incoming call: %v\n", err)
			res := sip.NewResponseFromRequest(req, 500, "Internal Server Error", nil)
			tx.Respond(res)
			return
		}
		sess = newSess

		// Store incoming call info in session
		sess.SetCallInfo("inbound", fromURI, toURI, callIDValue)
		if matchedTrunk != nil {
			sess.SetSIPAuthContext(
				"trunk",
				"",
				matchedTrunk.ID,
				matchedTrunk.Domain,
				matchedTrunk.Username,
				matchedTrunk.Password,
				matchedTrunk.Port,
			)
		}
		sess.UpdateState(session.StateIncoming)
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sess.ID,
			Category:  "sip",
			Name:      "sip_incoming_session_created",
			SIPCallID: callIDValue,
			State:     string(session.StateIncoming),
			Data:      map[string]interface{}{"from": fromURI, "to": toURI},
		})
		s.logSessionSnapshot(ctx, sess, "")

		// Store SIP transaction, request, and INVITE body for later response
		sess.SetIncomingInvite(tx, req, req.Body(), fromURI, toURI)

		// Send 180 Ringing
		ringingRes := sip.NewResponseFromRequest(req, 180, "Ringing", nil)
		ringingRes.AppendHeader(&sip.ContactHeader{
			Address: sip.Uri{Host: s.publicAddress, Port: s.sipPort},
		})
		tx.Respond(ringingRes)
		fmt.Printf("✅ Sent 100 Trying + 180 Ringing for session %s\n", sess.ID)
		s.logEvent(&logstore.Event{
			Timestamp:     time.Now(),
			SessionID:     sess.ID,
			Category:      "sip",
			Name:          "sip_ringing_sent",
			SIPStatusCode: 180,
			SIPCallID:     callIDValue,
		})

		// Notify browser about incoming call
		callerID := fromDisplayName
		if callerID == "" {
			callerID = fromURI
		}
		incomingTrunkID := int64(0)
		if matchedTrunk != nil {
			incomingTrunkID = matchedTrunk.ID
		}
		s.incomingNotifier.NotifyIncomingCall(sess.ID, callerID, toURI, incomingTrunkID)
		fmt.Printf("📲 Notified browser about incoming call from: %s\n", callerID)

		return
	}

	// If no session creator or notifier configured, reject the call
	// (sess is guaranteed to be nil here since we didn't create one)
	fmt.Printf("❌ Cannot handle incoming call - no session creator/notifier configured\n")
	res := sip.NewResponseFromRequest(req, 503, "Service Unavailable", nil)
	tx.Respond(res)
	s.logEvent(&logstore.Event{
		Timestamp:     time.Now(),
		SessionID:     "",
		Category:      "sip",
		Name:          "sip_invite_rejected_no_handler",
		SIPStatusCode: 503,
		SIPCallID:     callIDValue,
	})
}

// handleBYE handles BYE requests (call termination)
func (s *Server) handleBYE(req *sip.Request, tx sip.ServerTransaction) {
	ctx := context.Background()
	fmt.Printf("\n=== Received BYE Request ===\n")
	fmt.Printf("From: %s\n", req.From().Value())
	fmt.Printf("To: %s\n", req.To().Value())

	var callIDValue string
	if callID := req.CallID(); callID != nil {
		callIDValue = callID.Value()
		fmt.Printf("Call-ID: %s\n", callIDValue)
	}

	var fullPayloadID *int64
	if s.logFullSIP {
		fullPayloadID = s.storePayload(ctx, &logstore.PayloadRecord{
			SessionID:   "",
			Timestamp:   time.Now(),
			Kind:        "sip_message",
			ContentType: "application/sip",
			BodyText:    req.String(),
		})
	}
	if cseq := req.CSeq(); cseq != nil {
		fmt.Printf("CSeq: %s\n", cseq.Value())
	}
	fmt.Printf("Method: %s\n", req.Method)
	fmt.Printf("Request-URI: %s\n", req.Recipient.String())

	// Log Via headers (critical for response routing)
	fmt.Printf("\nVia Headers:\n")
	for i, via := range req.GetHeaders("Via") {
		fmt.Printf("  Via[%d]: %s\n", i, via.Value())
	}

	// Log source/destination
	if req.Source() != "" {
		fmt.Printf("Request Source: %s\n", req.Source())
	}
	if req.Destination() != "" {
		fmt.Printf("Request Destination: %s\n", req.Destination())
	}

	// Find session by Call-ID
	var sess *session.Session
	if s.sessionMgr != nil && callIDValue != "" {
		if foundSess, ok := s.sessionMgr.GetSessionBySIPCallID(callIDValue); ok {
			sess = foundSess
			fmt.Printf("✅ Found session: %s\n", sess.ID)
		} else {
			fmt.Printf("⚠️  Session not found for Call-ID: %s\n", callIDValue)
		}
	}

	byeSessionID := ""
	if sess != nil {
		byeSessionID = sess.ID
	}
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: byeSessionID,
		Category:  "sip",
		Name:      "sip_bye_received",
		SIPMethod: string(req.Method),
		SIPCallID: callIDValue,
		PayloadID: fullPayloadID,
	})

	// Create response
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	fmt.Printf("\n=== Sending BYE Response ===\n")
	fmt.Printf("Status: 200 OK\n")

	// Log response Via headers (should match request)
	fmt.Printf("Response Via Headers:\n")
	for i, via := range res.GetHeaders("Via") {
		fmt.Printf("  Via[%d]: %s\n", i, via.Value())
	}

	// Send response
	if err := tx.Respond(res); err != nil {
		fmt.Printf("❌ ERROR responding to BYE: %v\n", err)
	} else {
		fmt.Printf("✅ BYE 200 OK sent via ServerTransaction\n")
	}

	s.logEvent(&logstore.Event{
		Timestamp:     time.Now(),
		SessionID:     byeSessionID,
		Category:      "sip",
		Name:          "sip_bye_responded",
		SIPStatusCode: 200,
		SIPCallID:     callIDValue,
	})

	// Clean up session and notify clients
	if sess != nil {
		fmt.Printf("🔔 Notifying clients about hangup (session %s)\n", sess.ID)

		// Decrement public account refcount if applicable (before deleting session)
		authMode, accountKey, _, _, _, _, _ := sess.GetSIPAuthContext()
		if authMode == "public" && accountKey != "" && s.publicRegistry != nil {
			s.publicRegistry.DecrementRefCount(accountKey)
		}

		// Close media transports (RTP/RTCP) immediately so audio/video stops
		// flowing on both sides before we notify the browser.
		sess.CloseMediaTransports()

		// Close the WebRTC PeerConnection so the browser ICE/DTLS connection is
		// torn down right away – this is the reason only one side was cut before.
		if sess.PeerConnection != nil {
			if err := sess.PeerConnection.Close(); err != nil {
				fmt.Printf("⚠️ [%s] PeerConnection.Close() error: %v\n", sess.ID, err)
			} else {
				fmt.Printf("✅ [%s] PeerConnection closed\n", sess.ID)
			}
		}

		// Update session state and notify WebSocket/SSE clients
		sess.UpdateState(session.StateEnded)
		s.notifySessionStateChange(sess, session.StateEnded)
		s.logSessionSnapshot(ctx, sess, "sip_bye_received")

		fmt.Printf("✅ State change notification sent to client\n")

		// Delete session and cleanup any remaining resources
		if s.sessionMgr != nil {
			s.sessionMgr.DeleteSession(sess.ID)
			fmt.Printf("✅ Session cleaned up\n")
		}
	}

	fmt.Printf("============================\n\n")
}

// handleACK handles ACK requests
func (s *Server) handleACK(req *sip.Request, tx sip.ServerTransaction) {
	ctx := context.Background()
	// ACK confirms the INVITE transaction is complete
	fmt.Printf("Received ACK from: %s\n", req.From().Value())

	// Extract Call-ID to find the session
	var callIDValue string
	if callID := req.CallID(); callID != nil {
		callIDValue = callID.Value()
	}

	// Find session by Call-ID and set dialog state if not already set
	if s.sessionMgr != nil && callIDValue != "" {
		if sess, ok := s.sessionMgr.GetSessionBySIPCallID(callIDValue); ok {
			s.logEvent(&logstore.Event{
				Timestamp: time.Now(),
				SessionID: sess.ID,
				Category:  "sip",
				Name:      "sip_ack_received",
				SIPMethod: string(req.Method),
				SIPCallID: callIDValue,
			})
			// Only set if dialog state is empty (wasn't set by AcceptCall due to transaction error)
			if !sess.HasDialogState() {
				fmt.Printf("📞 [%s] Setting dialog state from ACK (fallback)\n", sess.ID)

				// Extract tags AND URIs from ACK
				fromTag := ""
				remotePartyURI := ""
				if fromHeader := req.From(); fromHeader != nil {
					if fromHeader.Params != nil {
						if tag, ok := fromHeader.Params.Get("tag"); ok {
							fromTag = tag
						}
					}
					remotePartyURI = fromHeader.Address.String()
					fmt.Printf("📞 [%s] ACK From (remote party): %s\n", sess.ID, remotePartyURI)
				}

				toTag := ""
				localPartyURI := ""
				if toHeader := req.To(); toHeader != nil {
					if toHeader.Params != nil {
						if tag, ok := toHeader.Params.Get("tag"); ok {
							toTag = tag
						}
					}
					localPartyURI = toHeader.Address.String()
					fmt.Printf("📞 [%s] ACK To (local party): %s\n", sess.ID, localPartyURI)
				}

				remoteContact := ""
				if contactHeaders := req.GetHeaders("Contact"); len(contactHeaders) > 0 {
					remoteContact = contactHeaders[0].Value()
				}

				// Extract Record-Route from original INVITE and convert to Route set
				routeSet := make([]string, 0)

				// Get original INVITE request from session
				if inviteReq, ok := sess.GetIncomingInviteRequest().(*sip.Request); ok && inviteReq != nil {
					recordRoutes := inviteReq.GetHeaders("Record-Route")
					fmt.Printf("📞 [%s] Original INVITE Record-Route headers: %d found\n", sess.ID, len(recordRoutes))

					// Reverse Record-Route to create Route set (UAS perspective)
					for i := len(recordRoutes) - 1; i >= 0; i-- {
						routeSet = append(routeSet, recordRoutes[i].Value())
						fmt.Printf("📞 [%s] RouteSet[%d]: %s\n", sess.ID, len(routeSet)-1, recordRoutes[i].Value())
					}
				} else {
					fmt.Printf("📞 [%s] ⚠️ No stored INVITE request to extract Record-Route\n", sess.ID)
				}

				// For incoming calls: we are callee, so swap tags
				dialogDomain, dialogPort := s.resolveDialogDomainPort(sess)
				sess.SetSIPDialogState(toTag, fromTag, remoteContact, dialogDomain, dialogPort, 1, routeSet)
				sess.UpdateState(session.StateActive)
				s.notifySessionStateChange(sess, session.StateActive)
				s.logDialogSnapshot(ctx, sess)
				s.logSessionSnapshot(ctx, sess, "")

				// CRITICAL: Update From/To with actual URIs from ACK
				sess.UpdateFromTo(remotePartyURI, localPartyURI)

				fmt.Printf("✅ [%s] Dialog state set from ACK - FromTag: %s, ToTag: %s, Contact: %s\n",
					sess.ID, toTag, fromTag, remoteContact)
				fromValue, toValue := sess.GetFromTo()
				fmt.Printf("✅ [%s] Updated URIs - From: %s, To: %s\n", sess.ID, fromValue, toValue)
			}
		}
	}
}

// handleOPTIONS handles OPTIONS requests (keep-alive / availability check)
func (s *Server) handleOPTIONS(req *sip.Request, tx sip.ServerTransaction) {
	ctx := context.Background()
	// Respond with 200 OK for OPTIONS (availability check)
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	res.AppendHeader(sip.NewHeader("Allow", "INVITE, ACK, BYE, CANCEL, OPTIONS, MESSAGE"))
	res.AppendHeader(sip.NewHeader("Accept", "application/sdp, text/plain"))

	if err := tx.Respond(res); err != nil {
		fmt.Printf("ERROR responding to OPTIONS: %v\n", err)
	}

	var callIDValue string
	if callID := req.CallID(); callID != nil {
		callIDValue = callID.Value()
	}
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: "",
		Category:  "sip",
		Name:      "sip_options_received",
		SIPMethod: string(req.Method),
		SIPCallID: callIDValue,
	})

	if s.logFullSIP {
		_ = s.storePayload(ctx, &logstore.PayloadRecord{
			SessionID:   "",
			Timestamp:   time.Now(),
			Kind:        "sip_message",
			ContentType: "application/sip",
			BodyText:    req.String(),
		})
	}
}

// handleMESSAGE handles SIP MESSAGE requests (instant messaging)
func (s *Server) handleMESSAGE(req *sip.Request, tx sip.ServerTransaction) {
	ctx := context.Background()
	// Extract From
	fromURI := ""
	fromDisplayName := ""
	if fromHeader := req.From(); fromHeader != nil {
		fromURI = fromHeader.Address.String()
		fromDisplayName = fromHeader.DisplayName
	}

	// Extract To
	toURI := ""
	if toHeader := req.To(); toHeader != nil {
		toURI = toHeader.Address.String()
	}

	// Extract Content-Type
	contentType := "text/plain"
	if ctHeaders := req.GetHeaders("Content-Type"); len(ctHeaders) > 0 {
		contentType = ctHeaders[0].Value()
	}

	// Extract message body
	body := string(req.Body())

	var callIDValue string
	if callID := req.CallID(); callID != nil {
		callIDValue = callID.Value()
	}

	payloadID := s.storePayload(ctx, &logstore.PayloadRecord{
		SessionID:   "",
		Timestamp:   time.Now(),
		Kind:        "sip_message",
		ContentType: contentType,
		BodyText:    body,
	})
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: "",
		Category:  "sip",
		Name:      "sip_message_received",
		SIPMethod: string(req.Method),
		SIPCallID: callIDValue,
		PayloadID: payloadID,
		Data:      map[string]interface{}{"from": fromURI, "to": toURI},
	})

	if s.logFullSIP {
		_ = s.storePayload(ctx, &logstore.PayloadRecord{
			SessionID:   "",
			Timestamp:   time.Now(),
			Kind:        "sip_message_full",
			ContentType: "application/sip",
			BodyText:    req.String(),
		})
	}

	// Handle @switch: message - trigger PLI after delay
	if strings.HasPrefix(body, "@switch") {
		fmt.Printf("🔀 Found @switch: message: %s, handling PLI Keyframe...\n", body)
		go s.handleSwitchMessage(body, toURI)
	}

	// Handle @fir or @pli message - trigger keyframe request
	if strings.HasPrefix(body, "@fir") || strings.HasPrefix(body, "@pli") {
		fmt.Printf("🔀 Found keyframe message: %s, handling keyframe request...\n", body)
		go s.handleKeyframeMessage(body, toURI)
	}

	// Determine caller display name
	caller := fromDisplayName
	if caller == "" {
		caller = fromURI
	}

	// Debug logging (controlled by DEBUG_SIP_MESSAGE env)
	if s.config.DebugSIPMessage {
		fmt.Printf("\n=== 💬 Received SIP MESSAGE ===\n")
		fmt.Printf("From: %s (%s)\n", fromURI, fromDisplayName)
		fmt.Printf("To: %s\n", toURI)
		fmt.Printf("Content-Type: %s\n", contentType)
		fmt.Printf("Content-Length: %d\n", len(body))
		fmt.Printf("Body: %s\n", body)
		fmt.Printf("--- Full Request ---\n")
		fmt.Printf("%s\n", req.String())
		fmt.Printf("================================\n\n")
	} else {
		// Minimal log when debug is off
		fmt.Printf("💬 MESSAGE from %s to %s (%d bytes)\n", caller, toURI, len(body))
	}

	// Send 200 OK response
	okRes := sip.NewResponseFromRequest(req, 200, "OK", nil)
	if err := tx.Respond(okRes); err != nil {
		fmt.Printf("ERROR responding to MESSAGE: %v\n", err)
	}

	// Notify WebSocket clients
	if s.messageNotifier != nil {
		s.messageNotifier.NotifySIPMessage(toURI, caller, body, contentType)
	}
}

// parseSwitchMessage parses @switch:queueNumber|agentUsername format
// Returns queueNumber and agentUsername
func parseSwitchMessage(body string) (queueNumber string, agentUsername string, ok bool) {
	// Expected format: @switch:14131|00025
	if !strings.HasPrefix(body, "@switch:") {
		return "", "", false
	}

	// Remove prefix
	data := strings.TrimPrefix(body, "@switch:")

	// Split by |
	parts := strings.SplitN(data, "|", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

// extractUsernameFromURI extracts the username from a SIP URI
// e.g., "sip:0900200002@domain.com" -> "0900200002"
// e.g., "<sip:0900200002@domain.com>" -> "0900200002"
// e.g., "0900200002" -> "0900200002"
func extractUsernameFromURI(uri string) string {
	// Remove angle brackets if present
	uri = strings.TrimPrefix(uri, "<")
	uri = strings.TrimSuffix(uri, ">")

	// Remove sip: or sips: prefix
	uri = strings.TrimPrefix(uri, "sip:")
	uri = strings.TrimPrefix(uri, "sips:")

	// Extract username (part before @)
	if atIdx := strings.Index(uri, "@"); atIdx != -1 {
		return uri[:atIdx]
	}

	return uri
}

func (s *Server) handleKeyframeMessage(body string, callerURI string) {
	// Determine if this is FIR or PLI request
	isFIR := strings.HasPrefix(body, "@fir")

	keyframeType := "PLI"
	if isFIR {
		keyframeType = "FIR"
	}

	fmt.Printf("📸 Handling %s keyframe request from message: %s\n", keyframeType, body)

	// Extract username from SIP URI (e.g., "sip:0900200002@domain" -> "0900200002")
	callerUsername := extractUsernameFromURI(callerURI)
	fmt.Printf("📸 %s request for caller: %s (username: %s)\n", keyframeType, callerURI, callerUsername)

	// Find session by caller username
	if s.sessionMgr == nil {
		fmt.Printf("⚠️ Session manager not available for %s handling\n", keyframeType)
		return
	}

	sess, found := s.sessionMgr.GetSessionByFromUsername(callerUsername)
	if !found {
		fmt.Printf("⚠️ No active session found for caller: %s\n", callerUsername)
		return
	}

	fmt.Printf("📍 Found session %s for caller %s, sending %s requests...\n", sess.ID, callerUsername, keyframeType)

	// Send keyframe requests to both Browser and Asterisk 1 sec
	for i := 0; i < 10; i++ {
		if isFIR {
			// Send FIR to both browser and Asterisk
			sess.SendFIRToWebRTC()
			sess.SendFIRToAsterisk()
		} else {
			// Send PLI to both browser and Asterisk
			sess.SendPLItoWebRTC()
			sess.SendPLIToAsteriskForced("sip-message")
		}

		time.Sleep(100 * time.Millisecond)
		if sess.GetState() == session.StateEnded {
			return
		}
	}

	fmt.Printf("✅ Sent %s (Browser + Asterisk) for session: %s\n", keyframeType, sess.ID)
}

// handleSwitchMessage handles @switch:xxxxx messages by sending PLI after delay
// callerURI is the To header from the SIP MESSAGE, which identifies the caller's session
func (s *Server) handleSwitchMessage(body string, callerURI string) {
	// 1. Parse @switch message
	queueNumber, agentUsername, ok := parseSwitchMessage(body)
	if !ok {
		fmt.Printf("⚠️ Invalid @switch message format: %s\n", body)
		// return
		queueNumber = "force send PLI"
		agentUsername = "force send PLI"
	}

	// Extract username from SIP URI (e.g., "sip:0900200002@domain" -> "0900200002")
	callerUsername := extractUsernameFromURI(callerURI)
	fmt.Printf("🔀 @switch message: queue=%s, agent=%s, caller=%s (username: %s)\n", queueNumber, agentUsername, callerURI, callerUsername)

	// 2. Find session by caller username (from SIP MESSAGE To header)
	if s.sessionMgr == nil {
		fmt.Printf("⚠️ Session manager not available for @switch handling\n")
		return
	}

	sess, found := s.sessionMgr.GetSessionByFromUsername(callerUsername)
	if !found {
		fmt.Printf("⚠️ No active session found for caller: %s\n", callerUsername)
		return
	}

	fmt.Printf("📍 Found session %s for caller %s (queue: %s, agent: %s)\n", sess.ID, callerUsername, queueNumber, agentUsername)

	// 3. Immediate fast-start kick before any optional delay.
	// Send FIR + PLI once to both endpoints to reduce first-keyframe latency.
	fmt.Printf("[%s] 🔀 Sending @switch: immediate FIR + PLI kick to both endpoints\n", sess.ID)
	sess.SendFIRToWebRTC()   // FIR to browser
	sess.SendFIRToAsterisk() // FIR to Asterisk
	sess.SendPLItoWebRTC()   // PLI to browser
	sess.SendPLIToAsteriskForced("switch")

	// 3.1 Enable temporary @switch blackout on SIP->WebRTC video path (if enabled).
	// Keep remote screen intentionally black until target video stabilizes
	// (keyframe received) or max wait timeout is reached.
	if s.config.SwitchVideoBlackoutEnabled {
		blackout := time.Duration(s.config.SwitchVideoBlackoutMS) * time.Millisecond
		if blackout <= 0 {
			blackout = 700 * time.Millisecond
		}
		maxWait := time.Duration(s.config.SwitchVideoBlackoutMaxWaitMS) * time.Millisecond
		if maxWait < blackout {
			maxWait = blackout
		}
		sess.StartSwitchVideoBlackout(blackout, maxWait, "switch")
	}

	// Enable the same temporary aggressive recovery policy used by reconnect/resume.
	sess.StartVideoRecoveryBurst("switch")

	if queueNumber != "force send PLI" {
		// 3.2 Optional delay (configurable via SWITCH_PLI_DELAY_MS)
		// Applied after immediate kick so first keyframe request is never delayed.
		delayMs := s.config.SwitchPLIDelayMS
		if delayMs > 0 {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}
	}

	// 4. Send FIR burst (6x, 50ms) to request keyframe with SPS/PPS quickly.
	fmt.Printf("[%s] 🔀 Sending @switch: FIR burst (6x @ 50ms)\n", sess.ID)
	for i := 0; i < 6; i++ {
		if sess.GetState() == session.StateEnded {
			return
		}
		sess.SendFIRToWebRTC()   // FIR to browser
		sess.SendFIRToAsterisk() // FIR to Asterisk
		time.Sleep(50 * time.Millisecond)
	}

	// 5. Send PLI burst (6x, 50ms) for redundancy and faster stabilization.
	fmt.Printf("[%s] 🔀 Sending @switch: PLI burst (6x @ 50ms)\n", sess.ID)
	for i := 0; i < 6; i++ {
		if sess.GetState() == session.StateEnded {
			return
		}
		sess.SendPLItoWebRTC() // PLI to browser
		sess.SendPLIToAsteriskForced("switch")
		time.Sleep(50 * time.Millisecond)
	}

	fmt.Printf("✅ Sent @switch immediate kick + FIR/PLI bursts (Browser + Asterisk) for session: %s\n", sess.ID)
}

// TriggerSwitchMessage triggers @switch handling from external callers (e.g. REST API).
// This method validates input, then runs the heavy switch workflow asynchronously.
func (s *Server) TriggerSwitchMessage(body string, callerURI string) error {
	body = strings.TrimSpace(body)
	callerURI = strings.TrimSpace(callerURI)

	if body == "" {
		return fmt.Errorf("switch message body is required")
	}
	if callerURI == "" {
		return fmt.Errorf("caller URI is required")
	}

	go s.handleSwitchMessage(body, callerURI)
	return nil
}

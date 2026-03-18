package sip

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"k2-gateway/internal/logstore"
	"k2-gateway/internal/session"
)

var (
	contentTypeHeaderSDPCall = sip.ContentTypeHeader("application/sdp")
)

type sipAuthParams struct {
	Domain    string
	Port      int
	Username  string
	Password  string
	Transport string
}

func (s *Server) resolveSessionSIPParams(sess *session.Session) (sipAuthParams, error) {
	mode, _, trunkID, domain, username, password, port := sess.GetSIPAuthContext()
	params := sipAuthParams{}

	switch mode {
	case "public":
		params.Domain = domain
		params.Port = port
		params.Username = username
		params.Password = password
		params.Transport = "tcp"

	case "trunk":
		if s.trunkManager == nil {
			return params, fmt.Errorf("trunk manager not available")
		}
		trunkIface, found := s.trunkManager.GetTrunkByID(trunkID)
		if !found {
			return params, fmt.Errorf("trunk %d not found", trunkID)
		}
		trunk, ok := trunkIface.(*Trunk)
		if !ok || trunk == nil {
			return params, fmt.Errorf("invalid trunk type for %d", trunkID)
		}
		params.Domain = trunk.Domain
		params.Port = trunk.Port
		params.Username = trunk.Username
		params.Password = trunk.Password
		params.Transport = trunk.Transport

	default:
		params.Domain = s.getActiveDomain()
		params.Port = s.getActivePort()
		params.Username = s.getActiveUsername()
		params.Password = s.getActivePassword()
		params.Transport = "tcp"
	}

	if params.Domain == "" {
		params.Domain = s.getActiveDomain()
	}
	if params.Port == 0 {
		params.Port = s.getActivePort()
	}
	if params.Username == "" {
		params.Username = s.getActiveUsername()
	}
	if params.Transport == "" {
		params.Transport = "tcp"
	}

	return params, nil
}

// MakeCall initiates an outbound SIP INVITE
func (s *Server) MakeCall(destination, from string, sess *session.Session) error {
	if s.sipClient == nil {
		return fmt.Errorf("SIP client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "sip",
		Name:      "sip_invite_build",
	})

	// Resolve SIP auth params for this session
	params, err := s.resolveSessionSIPParams(sess)
	if err != nil {
		return fmt.Errorf("failed to resolve SIP auth params: %w", err)
	}

	// Use configured username if from is empty
	if from == "" {
		from = params.Username
	}

	// Ensure no stale transports from a previous call remain bound to this session.
	sess.CloseMediaTransports()

	// Reset media state for new call (ensures fresh SSRC, SPS/PPS cache)
	sess.ResetMediaState()
	sess.StartVideoRTCPFallbackWindow(4*time.Second, "call-start")

	// For outbound calls: default Opus PT to 111 (will be updated from 200 OK answer if different)
	sess.SIPOpusPT = 111

	// Start RTP listener for this session
	rtpPort, err := s.startRTPListenerForSession(sess)
	if err != nil {
		return fmt.Errorf("failed to start RTP listener: %w", err)
	}
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			sess.CloseMediaTransports()
		}
	}()

	// Wait for SPS/PPS to be cached from WebRTC video stream (up to 5s)
	// This allows us to include sprop-parameter-sets in SDP for Linphone compatibility
	fmt.Printf("[%s] 📹 Waiting for video SPS/PPS before sending INVITE...\n", sess.ID)
	sess.WaitForSPSPPS(5 * time.Second)

	// Create SDP offer for the outbound call (with ICE-lite)
	// If SPS/PPS are available, they will be included in sprop-parameter-sets
	sdpOffer := s.createSDPOffer(rtpPort, sess)

	payloadID := s.storePayload(ctx, &logstore.PayloadRecord{
		SessionID:    sess.ID,
		Timestamp:    time.Now(),
		Kind:         "sip_sdp_offer",
		ContentType:  "application/sdp",
		BodyBytesB64: "",
		BodyText:     string(sdpOffer),
	})
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "sip",
		Name:      "sip_sdp_offer_created",
		PayloadID: payloadID,
	})

	// Create INVITE request
	inviteReq, err := s.createInviteRequestWithParams(destination, from, sdpOffer, params)
	if err != nil {
		return fmt.Errorf("failed to create INVITE request: %w", err)
	}

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "sip",
		Name:      "sip_invite_sent",
		SIPMethod: string(inviteReq.Method),
		SIPCallID: inviteReq.CallID().Value(),
	})

	// Update session state
	sess.UpdateState(session.StateConnecting)
	s.notifySessionStateChange(sess, session.StateConnecting)
	sess.SetCallInfo("outbound", from, destination, inviteReq.CallID().Value())

	fmt.Printf("[%s] Making outbound call to %s\n", sess.ID, destination)

	// Send INVITE using TransactionRequest for proper response handling
	tx, err := s.sipClient.TransactionRequest(ctx, inviteReq)
	if err != nil {
		sess.UpdateState(session.StateEnded)
		s.notifySessionStateChange(sess, session.StateEnded)
		return fmt.Errorf("failed to send INVITE: %w", err)
	}
	defer tx.Terminate()

	// Handle responses from the transaction
	for {
		select {
		case res := <-tx.Responses():
			if res == nil {
				continue
			}

			s.logEvent(&logstore.Event{
				Timestamp:     time.Now(),
				SessionID:     sess.ID,
				Category:      "sip",
				Name:          "sip_response_received",
				SIPMethod:     string(inviteReq.Method),
				SIPStatusCode: res.StatusCode,
				SIPCallID:     inviteReq.CallID().Value(),
			})

			fmt.Printf("[%s] Received SIP response: %d %s\n", sess.ID, res.StatusCode, res.Reason)

			switch {
			case res.StatusCode == 100:
				// Trying - continue waiting
				fmt.Printf("[%s] Call trying...\n", sess.ID)
				continue

			case res.StatusCode == 180 || res.StatusCode == 183:
				// Ringing - update state and continue waiting
				sess.UpdateState(session.StateRinging)
				s.notifySessionStateChange(sess, session.StateRinging)
				s.logEvent(&logstore.Event{
					Timestamp:     time.Now(),
					SessionID:     sess.ID,
					Category:      "sip",
					Name:          "sip_ringing",
					SIPStatusCode: res.StatusCode,
					State:         string(session.StateRinging),
				})
				fmt.Printf("[%s] Call ringing...\n", sess.ID)
				continue

			case res.StatusCode == 200:
				// Answered - call established
				sess.UpdateState(session.StateActive)
				s.notifySessionStateChange(sess, session.StateActive)
				s.logEvent(&logstore.Event{
					Timestamp:     time.Now(),
					SessionID:     sess.ID,
					Category:      "sip",
					Name:          "sip_200_ok",
					SIPStatusCode: res.StatusCode,
					State:         string(session.StateActive),
					SIPCallID:     inviteReq.CallID().Value(),
				})
				fmt.Printf("[%s] Call answered!\n", sess.ID)

				// Extract dialog state for BYE requests using helper
				dialogState, err := ExtractDialogStateFromResponse(res)
				if err != nil {
					return fmt.Errorf("failed to extract dialog state: %w", err)
				}

				// Extract fromTag from INVITE request (our tag)
				fromTag := ""
				if fromHeader := inviteReq.From(); fromHeader != nil && fromHeader.Params != nil {
					if tag, ok := fromHeader.Params.Get("tag"); ok {
						fromTag = tag
					}
				}

				// Store dialog state in session for BYE
				sess.SetSIPDialogState(fromTag, dialogState.ToTag, dialogState.RemoteContact, params.Domain, params.Port, 1, dialogState.RouteSet)
				fmt.Printf("[%s] Dialog state captured - FromTag: %s, ToTag: %s, Contact: %s, RouteSet: %v\n",
					sess.ID, fromTag, dialogState.ToTag, dialogState.RemoteContact, dialogState.RouteSet)
				s.logDialogSnapshot(ctx, sess)

				// Log SDP from Asterisk
				if len(res.Body()) > 0 {
					fmt.Printf("=== SDP Answer from Asterisk ===\n%s\n================================\n", string(res.Body()))

					answerPayloadID := s.storePayload(ctx, &logstore.PayloadRecord{
						SessionID:   sess.ID,
						Timestamp:   time.Now(),
						Kind:        "sip_sdp_answer",
						ContentType: "application/sdp",
						BodyText:    string(res.Body()),
					})
					s.logEvent(&logstore.Event{
						Timestamp:     time.Now(),
						SessionID:     sess.ID,
						Category:      "sip",
						Name:          "sip_sdp_answer_received",
						PayloadID:     answerPayloadID,
						SIPStatusCode: res.StatusCode,
						SIPCallID:     inviteReq.CallID().Value(),
					})

					// Parse Opus PT from 200 OK SDP answer (in case Asterisk uses different PT)
					opusPT := parseOpusPayloadType(res.Body())
					if opusPT > 0 && opusPT != sess.SIPOpusPT {
						fmt.Printf("[%s] 🎵 Updated Opus PT from answer: %d → %d\n", sess.ID, sess.SIPOpusPT, opusPT)
						sess.SIPOpusPT = opusPT
						s.logEvent(&logstore.Event{
							Timestamp: time.Now(),
							SessionID: sess.ID,
							Category:  "sip",
							Name:      "sip_opus_pt_updated",
							Data:      map[string]interface{}{"opus_pt": opusPT},
						})
					}

					// Parse Asterisk SDP to get RTP ports for forwarding WebRTC → Asterisk
					s.parseAsteriskSDPAndSetEndpoints(res.Body(), sess)
				}

				// Send ACK
				s.sendAckForInvite(inviteReq, res)
				s.logEvent(&logstore.Event{
					Timestamp: time.Now(),
					SessionID: sess.ID,
					Category:  "sip",
					Name:      "sip_ack_sent",
					SIPCallID: inviteReq.CallID().Value(),
				})
				s.logSessionSnapshot(ctx, sess, "")
				cleanupOnError = false
				return nil

			case res.StatusCode == 401 || res.StatusCode == 407:
				// Authentication required - handle in separate function
				fmt.Printf("[%s] Authentication required for INVITE\n", sess.ID)
				s.logEvent(&logstore.Event{
					Timestamp:     time.Now(),
					SessionID:     sess.ID,
					Category:      "sip",
					Name:          "sip_invite_auth_challenge",
					SIPStatusCode: res.StatusCode,
					SIPCallID:     inviteReq.CallID().Value(),
				})
				tx.Terminate() // Terminate current transaction before auth
				if err := s.handleInviteAuth(ctx, inviteReq, res, sess); err != nil {
					return err
				}
				cleanupOnError = false
				return nil

			default:
				// Other final responses (4xx, 5xx, 6xx)
				sess.UpdateState(session.StateEnded)
				s.notifySessionStateChange(sess, session.StateEnded)
				s.logEvent(&logstore.Event{
					Timestamp:     time.Now(),
					SessionID:     sess.ID,
					Category:      "sip",
					Name:          "sip_invite_failed",
					SIPStatusCode: res.StatusCode,
					SIPCallID:     inviteReq.CallID().Value(),
				})
				s.logSessionSnapshot(ctx, sess, "sip_invite_failed")
				return fmt.Errorf("call failed with status: %d %s", res.StatusCode, res.Reason)
			}

		case <-tx.Done():
			// Transaction completed without final response
			if err := tx.Err(); err != nil {
				sess.UpdateState(session.StateEnded)
				s.notifySessionStateChange(sess, session.StateEnded)
				s.logEvent(&logstore.Event{
					Timestamp: time.Now(),
					SessionID: sess.ID,
					Category:  "sip",
					Name:      "sip_invite_transaction_error",
					Data:      map[string]interface{}{"error": err.Error()},
				})
				s.logSessionSnapshot(ctx, sess, "sip_invite_transaction_error")
				return fmt.Errorf("transaction error: %w", err)
			}
			cleanupOnError = false
			return nil

		case <-ctx.Done():
			// Best-effort CANCEL to terminate the pending INVITE on the SIP peer
			s.trySendCancel(inviteReq, sess)

			sess.UpdateState(session.StateEnded)
			s.notifySessionStateChange(sess, session.StateEnded)
			s.logEvent(&logstore.Event{
				Timestamp: time.Now(),
				SessionID: sess.ID,
				Category:  "sip",
				Name:      "sip_invite_timeout",
			})
			s.logSessionSnapshot(ctx, sess, "sip_invite_timeout")
			return fmt.Errorf("call timed out")
		}
	}
}

// handleInviteAuth handles authentication for INVITE requests
func (s *Server) handleInviteAuth(ctx context.Context, originalReq *sip.Request, challenge *sip.Response, sess *session.Session) error {
	params, err := s.resolveSessionSIPParams(sess)
	if err != nil {
		return fmt.Errorf("failed to resolve SIP auth params: %w", err)
	}
	password := params.Password
	if password == "" {
		return fmt.Errorf("authentication required but no password configured")
	}

	fmt.Printf("[%s] Creating authenticated INVITE request\n", sess.ID)

	// Debug: Log WWW-Authenticate header
	for _, header := range challenge.GetHeaders("WWW-Authenticate") {
		fmt.Printf("[%s] WWW-Authenticate: %s\n", sess.ID, header.Value())
	}

	// Debug: Log credentials being used
	fmt.Printf("[%s] Using credentials - Username: %s, Password length: %d\n",
		sess.ID, params.Username, len(password))

	// Debug: Log the Request-URI
	fmt.Printf("[%s] Request URI: %s\n", sess.ID, originalReq.Recipient.String())

	// Clone the original request
	authReq := originalReq.Clone()

	// Remove old Via header and add new one with fresh branch
	authReq.RemoveHeader("Via")
	transportUpper := strings.ToUpper(params.Transport)
	viaHop := &sip.ViaHeader{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       transportUpper,
		Host:            s.publicAddress,
		Port:            s.sipPort,
	}
	viaParams := sip.NewParams()
	viaParams.Add("branch", sip.GenerateBranch())
	viaHop.Params = viaParams
	authReq.PrependHeader(viaHop)

	// Update CSeq to 2 for the authenticated request
	authReq.RemoveHeader("CSeq")
	authReq.AppendHeader(sip.NewHeader("CSeq", "2 INVITE"))

	// Ensure compatibility headers are present
	if len(authReq.GetHeaders("User-Agent")) == 0 {
		authReq.AppendHeader(sip.NewHeader("User-Agent", "LinphoneAndroid/4.6.0 (K2-Gateway)"))
	}
	if len(authReq.GetHeaders("Allow")) == 0 {
		authReq.AppendHeader(sip.NewHeader("Allow", "INVITE, ACK, CANCEL, OPTIONS, BYE, REFER, NOTIFY, MESSAGE, SUBSCRIBE, INFO, PRACK, UPDATE"))
	}
	if len(authReq.GetHeaders("Supported")) == 0 {
		authReq.AppendHeader(sip.NewHeader("Supported", "replaces, outbound, 100rel"))
	}

	// Remove all Content-Type headers and add new one
	for len(authReq.GetHeaders("Content-Type")) > 0 {
		authReq.RemoveHeader("Content-Type")
	}
	authReq.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))

	// Create digest credentials
	digest := sipgo.DigestAuth{
		Username: params.Username,
		Password: password,
	}

	// Debug: Check if SDP body is present
	fmt.Printf("[%s] Auth request body length: %d\n", sess.ID, len(authReq.Body()))
	if len(authReq.Body()) == 0 {
		fmt.Printf("[%s] WARNING: Auth request has no body!\n", sess.ID)
	}

	// Check Content-Type header
	contentTypeHeaders := authReq.GetHeaders("Content-Type")
	fmt.Printf("[%s] Content-Type headers count: %d\n", sess.ID, len(contentTypeHeaders))

	fmt.Printf("[%s] Sending authenticated INVITE with new Via/CSeq\n", sess.ID)

	// Use DoDigestAuth with the cloned request
	res, err := s.sipClient.DoDigestAuth(ctx, authReq, challenge, digest)
	if err != nil {
		sess.UpdateState(session.StateEnded)
		s.notifySessionStateChange(sess, session.StateEnded)
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sess.ID,
			Category:  "sip",
			Name:      "sip_invite_auth_failed",
			Data:      map[string]interface{}{"error": err.Error()},
		})
		s.logSessionSnapshot(ctx, sess, "sip_invite_auth_failed")
		return fmt.Errorf("authenticated INVITE failed: %w", err)
	}

	fmt.Printf("[%s] Auth INVITE response: %d %s\n", sess.ID, res.StatusCode, res.Reason)

	// Handle response
	switch {
	case res.StatusCode >= 100 && res.StatusCode < 200:
		// Provisional response (100, 180, 183, etc.)
		if res.StatusCode == 180 || res.StatusCode == 183 {
			sess.UpdateState(session.StateRinging)
			s.notifySessionStateChange(sess, session.StateRinging)
			fmt.Printf("[%s] Call ringing...\n", sess.ID)
		}
		return nil

	case res.StatusCode == 200:
		// Answered - call established
		sess.UpdateState(session.StateActive)
		s.notifySessionStateChange(sess, session.StateActive)
		fmt.Printf("[%s] Call answered (authenticated)!\n", sess.ID)

		// Extract dialog state for BYE requests using helper
		dialogState, err := ExtractDialogStateFromResponse(res)
		if err != nil {
			return fmt.Errorf("failed to extract dialog state: %w", err)
		}

		// Extract fromTag from INVITE request (our tag)
		fromTag := ""
		if fromHeader := authReq.From(); fromHeader != nil && fromHeader.Params != nil {
			if tag, ok := fromHeader.Params.Get("tag"); ok {
				fromTag = tag
			}
		}

		// Store dialog state in session for BYE
		sess.SetSIPDialogState(fromTag, dialogState.ToTag, dialogState.RemoteContact, params.Domain, params.Port, 2, dialogState.RouteSet)
		fmt.Printf("[%s] Dialog state captured - FromTag: %s, ToTag: %s, Contact: %s, RouteSet: %v\n",
			sess.ID, fromTag, dialogState.ToTag, dialogState.RemoteContact, dialogState.RouteSet)
		s.logDialogSnapshot(ctx, sess)

		// Log SDP from Asterisk
		if len(res.Body()) > 0 {
			fmt.Printf("=== SDP Answer from Asterisk ===\n%s\n================================\n", string(res.Body()))

			answerPayloadID := s.storePayload(ctx, &logstore.PayloadRecord{
				SessionID:   sess.ID,
				Timestamp:   time.Now(),
				Kind:        "sip_sdp_answer",
				ContentType: "application/sdp",
				BodyText:    string(res.Body()),
			})
			s.logEvent(&logstore.Event{
				Timestamp:     time.Now(),
				SessionID:     sess.ID,
				Category:      "sip",
				Name:          "sip_sdp_answer_received",
				PayloadID:     answerPayloadID,
				SIPStatusCode: res.StatusCode,
			})

			// Parse Asterisk SDP to get RTP ports for forwarding WebRTC → Asterisk
			s.parseAsteriskSDPAndSetEndpoints(res.Body(), sess)
		}

		// Send ACK (use authReq for proper Via/CSeq)
		s.sendAckForInvite(authReq, res)
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sess.ID,
			Category:  "sip",
			Name:      "sip_ack_sent",
		})
		s.logSessionSnapshot(ctx, sess, "")
		return nil

	case res.StatusCode == 401 || res.StatusCode == 407:
		// Still unauthorized - credentials might be wrong
		sess.UpdateState(session.StateEnded)
		s.notifySessionStateChange(sess, session.StateEnded)
		s.logEvent(&logstore.Event{
			Timestamp:     time.Now(),
			SessionID:     sess.ID,
			Category:      "sip",
			Name:          "sip_invite_auth_rejected",
			SIPStatusCode: res.StatusCode,
			Data:          map[string]interface{}{"reason": res.Reason},
		})
		s.logSessionSnapshot(ctx, sess, "sip_invite_auth_rejected")
		return fmt.Errorf("authentication failed - check username/password: %d %s", res.StatusCode, res.Reason)

	default:
		// Other final responses (4xx, 5xx, 6xx)
		sess.UpdateState(session.StateEnded)
		s.notifySessionStateChange(sess, session.StateEnded)
		s.logEvent(&logstore.Event{
			Timestamp:     time.Now(),
			SessionID:     sess.ID,
			Category:      "sip",
			Name:          "sip_invite_auth_failed",
			SIPStatusCode: res.StatusCode,
			Data:          map[string]interface{}{"reason": res.Reason},
		})
		s.logSessionSnapshot(ctx, sess, "sip_invite_auth_failed")
		return fmt.Errorf("authenticated call failed: %d %s", res.StatusCode, res.Reason)
	}
}

// Hangup terminates a SIP call by sending BYE to Asterisk
func (s *Server) Hangup(sess *session.Session) error {
	fmt.Printf("\n=== [%s] Hangup Request ===\n", sess.ID)
	direction, from, to, sipCallID := sess.GetCallInfo()
	fromTag, toTag, remoteContact, _, cseq, _, _ := sess.GetSIPDialogState()
	fmt.Printf("[%s] Direction: '%s'\n", sess.ID, direction)
	fmt.Printf("[%s] SIPCallID: '%s'\n", sess.ID, sipCallID)
	fmt.Printf("[%s] SIPFromTag: '%s'\n", sess.ID, fromTag)
	fmt.Printf("[%s] SIPToTag: '%s'\n", sess.ID, toTag)
	fmt.Printf("[%s] SIPRemoteContact: '%s'\n", sess.ID, remoteContact)
	fmt.Printf("[%s] SIPCSeq: %d\n", sess.ID, cseq)
	fmt.Printf("[%s] From: '%s', To: '%s'\n", sess.ID, from, to)
	fmt.Printf("[%s] sipClient is nil: %v\n", sess.ID, s.sipClient == nil)

	if s.sipClient == nil {
		fmt.Printf("[%s] ❌ Cannot send BYE: sipClient is nil\n", sess.ID)
		return nil
	}

	if sipCallID == "" {
		fmt.Printf("[%s] ❌ Cannot send BYE: SIPCallID is empty\n", sess.ID)
		return nil
	}

	fmt.Printf("[%s] ✅ Proceeding to send SIP BYE to Asterisk\n", sess.ID)

	// Send BYE request to Asterisk
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create BYE request
	byeReq, err := s.createBYERequest(sess)
	if err != nil {
		fmt.Printf("[%s] Error creating BYE request: %v\n", sess.ID, err)
	} else {
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sess.ID,
			Category:  "sip",
			Name:      "sip_bye_build",
			SIPCallID: sipCallID,
		})
		// Send BYE using TransactionRequest
		tx, err := s.sipClient.TransactionRequest(ctx, byeReq)
		if err != nil {
			fmt.Printf("[%s] Error sending BYE: %v\n", sess.ID, err)
		} else {
			s.logEvent(&logstore.Event{
				Timestamp: time.Now(),
				SessionID: sess.ID,
				Category:  "sip",
				Name:      "sip_bye_sent",
				SIPCallID: sipCallID,
			})
			// Wait for response
			select {
			case res := <-tx.Responses():
				if res != nil {
					fmt.Printf("[%s] BYE response: %d %s\n", sess.ID, res.StatusCode, res.Reason)
					s.logEvent(&logstore.Event{
						Timestamp:     time.Now(),
						SessionID:     sess.ID,
						Category:      "sip",
						Name:          "sip_bye_response",
						SIPStatusCode: res.StatusCode,
						SIPCallID:     sipCallID,
					})
				}
			case <-tx.Done():
				// Transaction completed
				fmt.Printf("[%s] BYE transaction completed\n", sess.ID)
			case <-ctx.Done():
				fmt.Printf("[%s] BYE timed out\n", sess.ID)
			}
			tx.Terminate()
		}
	}

	// Close media transports (RTP/RTCP UDP sockets) for this session.
	sess.CloseMediaTransports()

	// Close the WebRTC PeerConnection so the browser ICE/DTLS connection is
	// torn down immediately – without this the browser side stays connected
	// even after receiving the SIP BYE 200 OK.
	if sess.PeerConnection != nil {
		if err := sess.PeerConnection.Close(); err != nil {
			fmt.Printf("[%s] Warning: PeerConnection.Close() error: %v\n", sess.ID, err)
		} else {
			fmt.Printf("[%s] ✅ PeerConnection closed\n", sess.ID)
		}
	}

	sess.UpdateState(session.StateEnded)
	s.notifySessionStateChange(sess, session.StateEnded)

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "sip",
		Name:      "session_ended",
	})
	s.logSessionSnapshot(ctx, sess, "sip_hangup")
	fmt.Printf("[%s] Call terminated\n", sess.ID)

	return nil
}

// AcceptCall accepts an incoming SIP call by sending 200 OK
func (s *Server) AcceptCall(sess *session.Session) error {
	fmt.Printf("\n=== [%s] Accept Incoming Call ===\n", sess.ID)

	// Ensure no stale transports from a previous call remain bound to this session.
	sess.CloseMediaTransports()

	// Reset media state for new call (ensures fresh SSRC, SPS/PPS cache)
	sess.ResetMediaState()
	sess.StartVideoRTCPFallbackWindow(4*time.Second, "call-start")

	// Get stored SIP transaction
	storedTx, storedReq, inviteBody, _, _ := sess.GetIncomingInvite()
	tx, ok := storedTx.(sip.ServerTransaction)
	if !ok || tx == nil {
		return fmt.Errorf("no stored SIP transaction for incoming call")
	}

	// Get stored original INVITE request
	req, ok := storedReq.(*sip.Request)
	if !ok || req == nil {
		return fmt.Errorf("no stored SIP request for incoming call")
	}

	ctx := context.Background()
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "sip",
		Name:      "sip_inbound_accept_start",
		SIPCallID: req.CallID().Value(),
	})

	// Start RTP listeners for this session
	rtpPort, err := s.startRTPListenerForSession(sess)
	if err != nil {
		// Send 500 error
		errRes := sip.NewResponseFromRequest(req, 500, "Internal Server Error", nil)
		tx.Respond(errRes)
		return fmt.Errorf("failed to start RTP listener: %w", err)
	}
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			sess.CloseMediaTransports()
		}
	}()

	// CRITICAL: Parse incoming INVITE SDP to get Asterisk's RTP endpoints AND Opus PT
	if len(inviteBody) > 0 {
		fmt.Printf("[%s] 📡 Parsing INVITE SDP to get Asterisk endpoints + Opus PT...\n", sess.ID)

		invitePayloadID := s.storePayload(ctx, &logstore.PayloadRecord{
			SessionID:   sess.ID,
			Timestamp:   time.Now(),
			Kind:        "sip_sdp_offer",
			ContentType: "application/sdp",
			BodyText:    string(inviteBody),
		})
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sess.ID,
			Category:  "sip",
			Name:      "sip_inbound_invite_sdp_received",
			PayloadID: invitePayloadID,
			SIPCallID: req.CallID().Value(),
		})

		// Parse Opus payload type from INVITE SDP
		opusPT := parseOpusPayloadType(inviteBody)
		if opusPT > 0 {
			sess.SIPOpusPT = opusPT
			fmt.Printf("[%s] 🎵 Negotiated Opus payload type: %d (from INVITE SDP)\n", sess.ID, opusPT)
			s.logEvent(&logstore.Event{
				Timestamp: time.Now(),
				SessionID: sess.ID,
				Category:  "sip",
				Name:      "sip_opus_pt_negotiated_from_invite",
				Data:      map[string]interface{}{"opus_pt": opusPT},
			})
		} else {
			// Fallback to default if not found in INVITE
			sess.SIPOpusPT = 111
			fmt.Printf("[%s] ⚠️ No Opus in INVITE SDP, defaulting to PT=111\n", sess.ID)
		}

		s.parseAsteriskSDPAndSetEndpoints(inviteBody, sess)
	} else {
		fmt.Printf("[%s] ⚠️ No INVITE SDP to parse - WebRTC→Asterisk may not work\n", sess.ID)
		sess.SIPOpusPT = 111 // Default
	}

	// Create SDP answer using negotiated Opus PT and constraints from INVITE offer.
	sdpOffer := s.createSDPAnswerForInvite(rtpPort, sess, inviteBody)

	// Create 200 OK response using original request
	okRes := sip.NewResponseFromRequest(req, 200, "OK", sdpOffer)
	okRes.AppendHeader(&sip.ContactHeader{
		Address: sip.Uri{Host: s.publicAddress, Port: s.sipPort},
	})
	okRes.AppendHeader(&contentTypeHeaderSDPCall)

	// Send 200 OK
	if err := tx.Respond(okRes); err != nil {
		// Proxy/retransmission timing can report "transaction terminated"
		// even though ACK fallback can still complete dialog establishment.
		if isBenignIncomingAcceptRespondError(err) {
			fmt.Printf("⚠️ [%s] Non-fatal 200 OK response warning: %v\n", sess.ID, err)
			s.logEvent(&logstore.Event{
				Timestamp: time.Now(),
				SessionID: sess.ID,
				Category:  "sip",
				Name:      "sip_200_ok_send_warning",
				SIPCallID: req.CallID().Value(),
				Data:      map[string]interface{}{"warning": err.Error()},
			})
		} else {
			return fmt.Errorf("failed to send 200 OK: %w", err)
		}
	}

	answerPayloadID := s.storePayload(ctx, &logstore.PayloadRecord{
		SessionID:   sess.ID,
		Timestamp:   time.Now(),
		Kind:        "sip_sdp_answer",
		ContentType: "application/sdp",
		BodyText:    string(sdpOffer),
	})
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "sip",
		Name:      "sip_200_ok_sent",
		PayloadID: answerPayloadID,
		SIPCallID: req.CallID().Value(),
	})

	// Extract dialog state from INVITE request
	dialogState, _ := ExtractDialogStateFromINVITE(req)

	// Extract fromTag from INVITE (caller's tag)
	fromTag := dialogState.FromTag

	// Extract toTag from 200 OK response (our tag)
	toTag := ""
	if toHeader := okRes.To(); toHeader != nil && toHeader.Params != nil {
		if tag, ok := toHeader.Params.Get("tag"); ok {
			toTag = tag
		}
	}

	remoteContact := dialogState.RemoteContact
	routeSet := dialogState.RouteSet

	fmt.Printf("📞 [%s] INVITE Record-Route headers: %d found\n", sess.ID, len(routeSet))
	for i, r := range routeSet {
		fmt.Printf("📞 [%s] RouteSet[%d]: %s\n", sess.ID, i, r)
	}

	// For incoming calls: swap fromTag/toTag because we are the callee
	dialogDomain, dialogPort := s.resolveDialogDomainPort(sess)
	sess.SetSIPDialogState(toTag, fromTag, remoteContact, dialogDomain, dialogPort, 1, routeSet)
	fmt.Printf("✅ [%s] Dialog state stored - FromTag: %s, ToTag: %s, Contact: %s\n",
		sess.ID, toTag, fromTag, remoteContact)
	s.logDialogSnapshot(ctx, sess)

	// Update session state
	sess.UpdateState(session.StateActive)
	s.notifySessionStateChange(sess, session.StateActive)
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "sip",
		Name:      "session_state_changed",
		State:     string(session.StateActive),
	})
	s.logSessionSnapshot(ctx, sess, "")

	// Clear stored transaction and request
	sess.ClearIncomingInvite()

	cleanupOnError = false
	fmt.Printf("✅ [%s] Incoming call accepted - 200 OK sent\n", sess.ID)
	return nil
}

func isBenignIncomingAcceptRespondError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "transaction terminated")
}

func isBenignIncomingRejectRespondError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "transaction terminated")
}

// RejectCall rejects an incoming SIP call by sending 486 Busy Here
func (s *Server) RejectCall(sess *session.Session, reason string) error {
	fmt.Printf("\n=== [%s] Reject Incoming Call ===\n", sess.ID)

	// Get stored SIP transaction
	storedTx, _, _, _, _ := sess.GetIncomingInvite()
	tx, ok := storedTx.(sip.ServerTransaction)
	if !ok || tx == nil {
		return fmt.Errorf("no stored SIP transaction for incoming call")
	}

	// Send 486 Busy Here (or 603 Decline)
	code := 486
	reasonPhrase := "Busy Here"
	if reason == "decline" {
		code = 603
		reasonPhrase = "Decline"
	}

	ctx := context.Background()
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "sip",
		Name:      "sip_inbound_rejected",
		Data:      map[string]interface{}{"reason": reason, "status": code},
	})

	rejectRes := sip.NewResponse(code, reasonPhrase)
	if err := tx.Respond(rejectRes); err != nil {
		if isBenignIncomingRejectRespondError(err) {
			fmt.Printf("⚠️ [%s] Reject response warning: %v\n", sess.ID, err)
			s.logEvent(&logstore.Event{
				Timestamp: time.Now(),
				SessionID: sess.ID,
				Category:  "sip",
				Name:      "sip_reject_send_warning",
				Data: map[string]interface{}{
					"reason": reason,
					"status": code,
					"error":  err.Error(),
				},
			})
		} else {
			return fmt.Errorf("failed to send reject response: %w", err)
		}
	}

	// Update session state
	sess.UpdateState(session.StateEnded)
	s.notifySessionStateChange(sess, session.StateEnded)
	s.logSessionSnapshot(ctx, sess, "sip_rejected")

	// Clear stored transaction
	sess.ClearIncomingInvite()

	fmt.Printf("✅ [%s] Incoming call rejected (%d %s)\n", sess.ID, code, reasonPhrase)
	return nil
}

// SendDTMF sends DTMF tones via RFC 2833 RTP events
func (s *Server) SendDTMF(sess *session.Session, digits string) error {
	_, _, _, sipCallID := sess.GetCallInfo()
	if s.sipClient == nil || sipCallID == "" {
		return fmt.Errorf("no active call")
	}

	fmt.Printf("📞 [%s] SendDTMF: Sending digits '%s' via RFC 2833\n", sess.ID, digits)

	// Send each digit as an RFC 2833 RTP event
	for _, digit := range digits {
		if err := SendDTMFTone(sess, digit); err != nil {
			return fmt.Errorf("failed to send DTMF digit '%c': %w", digit, err)
		}

		// Inter-digit delay (100ms between tones)
		time.Sleep(DTMFInterDigitDelay)
	}

	fmt.Printf("📞 [%s] SendDTMF: All digits sent successfully\n", sess.ID)
	return nil
}

// createInviteRequest creates a SIP INVITE request (legacy)
func (s *Server) createInviteRequest(destination, from string, sdpBody []byte) (*sip.Request, error) {
	params := sipAuthParams{
		Domain:    s.getActiveDomain(),
		Port:      s.getActivePort(),
		Username:  s.getActiveUsername(),
		Password:  s.getActivePassword(),
		Transport: "tcp",
	}
	return s.createInviteRequestWithParams(destination, from, sdpBody, params)
}

// createInviteRequestWithParams creates a SIP INVITE request with explicit SIP params
func (s *Server) createInviteRequestWithParams(destination, from string, sdpBody []byte, params sipAuthParams) (*sip.Request, error) {
	domain := params.Domain
	port := params.Port
	transportUpper := strings.ToUpper(params.Transport)
	transportLower := strings.ToLower(params.Transport)
	fromUser := normalizeSIPUser(from)

	// Resolve destination domain
	ips, err := net.LookupIP(domain)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve SIP domain %s: %w", domain, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP addresses found for domain %s", domain)
	}
	resolvedIP := ips[0].String()

	// Extract username from destination (could be username, username@host, or full SIP URI)
	username := destination
	// Try to parse as SIP URI to extract username
	uriStr := destination
	if !strings.HasPrefix(uriStr, "sip:") && !strings.HasPrefix(uriStr, "sips:") {
		uriStr = "sip:" + uriStr
	}

	var parsedURI sip.Uri
	if err := sip.ParseUri(uriStr, &parsedURI); err == nil && parsedURI.User != "" {
		// Successfully parsed as URI with username
		username = parsedURI.User
	}
	// For INVITE, always use domain as host (not the host from destination)
	// NO PORT in Request-URI (like Linphone)
	recipient := sip.Uri{
		User: username,
		Host: domain,
		Port: 0,
	}

	// Create INVITE request
	req := sip.NewRequest(sip.INVITE, recipient)
	req.SetBody(sdpBody)

	// Add Via header with TCP transport
	viaHop := &sip.ViaHeader{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       transportUpper,
		Host:            s.publicAddress,
		Port:            s.sipPort,
	}
	viaParams := sip.NewParams()
	viaParams.Add("branch", sip.GenerateBranch())
	viaParams.Add("rport", "")
	viaHop.Params = viaParams
	req.AppendHeader(viaHop)

	// Add From header
	fromUri := sip.Uri{
		User: fromUser,
		Host: domain,
	}
	fromParams := sip.NewParams()
	fromParams.Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(&sip.FromHeader{
		Address: fromUri,
		Params:  fromParams,
	})

	// Add To header
	req.AppendHeader(&sip.ToHeader{
		Address: recipient,
	})

	// Add Call-ID
	callID := fmt.Sprintf("%s@%s", sip.GenerateTagN(16), s.publicAddress)
	req.AppendHeader(sip.NewHeader("Call-ID", callID))

	// Add CSeq
	req.AppendHeader(sip.NewHeader("CSeq", "1 INVITE"))

	// Add Max-Forwards
	req.AppendHeader(sip.NewHeader("Max-Forwards", "70"))

	// Add Supported header
	req.AppendHeader(sip.NewHeader("Supported", "outbound"))

	// Add Allow header
	req.AppendHeader(sip.NewHeader("Allow", "INVITE, ACK, CANCEL, OPTIONS, BYE, REFER, NOTIFY, MESSAGE, SUBSCRIBE, INFO, UPDATE"))

	// Add Content-Type
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))

	// Add Contact header
	contactUri := sip.Uri{
		User: fromUser,
		Host: s.publicAddress,
		Port: s.sipPort,
	}
	contactUriParams := sip.NewParams()
	contactUriParams.Add("transport", transportLower)
	contactUri.UriParams = contactUriParams
	req.AppendHeader(&sip.ContactHeader{
		Address: contactUri,
	})

	// Add User-Agent
	req.AppendHeader(sip.NewHeader("User-Agent", "TTRS-K2Gateway/1.0 (belle-sip/1.4.2)"))

	// Set destination
	destinationAddr := fmt.Sprintf("%s:%d", resolvedIP, port)
	req.SetDestination(destinationAddr)

	// CRITICAL: Force transport to prevent sipgo DoDigestAuth from switching transports
	// DoDigestAuth removes Via header and re-adds it, which can cause transport to switch
	// to default (UDP) if not explicitly set, leading to "context deadline exceeded" errors
	req.SetTransport(transportUpper)

	return req, nil
}

func normalizeSIPUser(user string) string {
	u := strings.TrimSpace(user)
	if u == "" {
		return ""
	}
	// Allow passing full SIP URIs (sip:user@host)
	uriStr := u
	if strings.HasPrefix(uriStr, "sip:") || strings.HasPrefix(uriStr, "sips:") {
		var parsed sip.Uri
		if err := sip.ParseUri(uriStr, &parsed); err == nil && parsed.User != "" {
			u = parsed.User
		}
	}
	// If passed as user@domain, keep only user
	if at := strings.IndexByte(u, '@'); at > 0 {
		u = u[:at]
	}
	// Strip auto- prefix if any
	u = strings.TrimPrefix(u, "auto-")
	return u
}

// resolveDialogDomainPort determines the best SIP domain/port source for dialog requests.
// Priority: dialog state -> session auth context -> active static config.
func (s *Server) resolveDialogDomainPort(sess *session.Session) (string, int) {
	_, _, _, _, _, dialogDomain, dialogPort := sess.GetSIPDialogState()
	if dialogDomain != "" {
		if dialogPort == 0 {
			dialogPort = s.getActivePort()
		}
		if dialogPort == 0 {
			dialogPort = 5060
		}
		return dialogDomain, dialogPort
	}

	_, _, _, authDomain, _, _, authPort := sess.GetSIPAuthContext()
	if authDomain != "" {
		if authPort == 0 {
			authPort = s.getActivePort()
		}
		if authPort == 0 {
			authPort = 5060
		}
		return authDomain, authPort
	}

	fallbackDomain := s.getActiveDomain()
	fallbackPort := s.getActivePort()
	if fallbackPort == 0 {
		fallbackPort = 5060
	}
	return fallbackDomain, fallbackPort
}

// createBYERequest creates a SIP BYE request for terminating a call
func (s *Server) createBYERequest(sess *session.Session) (*sip.Request, error) {
	// Get dialog state (contact, route set, tags)
	direction, from, to, sipCallID := sess.GetCallInfo()
	fromTag, toTag, remoteContact, routeSet, dialogCSeq, dialogDomain, dialogPort := sess.GetSIPDialogState()
	_, _, _, authDomain, _, _, authPort := sess.GetSIPAuthContext()

	domain := ""
	port := 0
	source := ""
	if dialogDomain != "" {
		domain = dialogDomain
		port = dialogPort
		source = "dialog"
	} else if authDomain != "" {
		domain = authDomain
		port = authPort
		source = "auth"
	}

	var recipient sip.Uri
	if remoteContact != "" {
		// Parse Contact header for Request-URI
		contactStr := remoteContact
		// Remove angle brackets if present
		contactStr = strings.TrimPrefix(contactStr, "<")
		contactStr = strings.TrimSuffix(contactStr, ">")
		var parsedURI sip.Uri
		if err := sip.ParseUri(contactStr, &parsedURI); err != nil {
			fmt.Printf("[%s] ⚠️  Failed to parse remote contact '%s': %v\n", sess.ID, remoteContact, err)
			recipient = sip.Uri{
				User: to,
				Host: domain,
			}
		} else {
			recipient = parsedURI
			if domain == "" && parsedURI.Host != "" {
				domain = parsedURI.Host
				port = parsedURI.Port
				source = "contact"
			}
		}
	} else {
		recipient = sip.Uri{
			User: to,
			Host: domain,
		}
	}

	if domain == "" {
		domain = s.getActiveDomain()
		source = "fallback"
	}
	if domain == "" {
		return nil, fmt.Errorf("no SIP domain available for BYE request")
	}
	if port == 0 {
		port = s.getActivePort()
	}
	if port == 0 {
		port = 5060
	}

	if recipient.Host == "" {
		recipient.Host = domain
	}

	fmt.Printf("[%s] BYE target source=%s domain=%s port=%d\n", sess.ID, source, domain, port)

	// Resolve destination domain
	ips, err := net.LookupIP(domain)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve SIP domain %s: %w", domain, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP addresses found for domain %s", domain)
	}
	resolvedIP := ips[0].String()

	fmt.Printf("[%s] BYE Request-URI: %s\n", sess.ID, recipient.String())

	// Create BYE request
	req := sip.NewRequest(sip.BYE, recipient)

	// Add Via header
	viaHop := &sip.ViaHeader{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       "TCP",
		Host:            s.publicAddress,
		Port:            s.sipPort,
	}
	viaParams := sip.NewParams()
	viaParams.Add("branch", sip.GenerateBranch())
	viaHop.Params = viaParams
	req.AppendHeader(viaHop)

	// Add Route headers from Record-Route (if any)
	if len(routeSet) > 0 {
		fmt.Printf("[%s] Adding %d Route headers to BYE\n", sess.ID, len(routeSet))
		for _, route := range routeSet {
			req.AppendHeader(sip.NewHeader("Route", route))
		}
	}

	// Determine local/remote URIs and tags based on call direction
	var localURI, remoteURI string
	var localTag, remoteTag string

	if direction == "inbound" {
		localURI = to
		remoteURI = from
		localTag = fromTag
		remoteTag = toTag
		fmt.Printf("[%s] Inbound call - swapping From/To for BYE (localTag=%s, remoteTag=%s)\n", sess.ID, localTag, remoteTag)
	} else {
		localURI = from
		remoteURI = to
		localTag = fromTag
		remoteTag = toTag
	}

	// Add From header (our local identity)
	var fromUri sip.Uri
	if strings.HasPrefix(localURI, "sip:") {
		if err := sip.ParseUri(localURI, &fromUri); err != nil {
			fmt.Printf("[%s] Failed to parse From URI '%s': %v\n", sess.ID, localURI, err)
			fromUri = sip.Uri{User: localURI, Host: domain}
		}
	} else {
		fromUri = sip.Uri{User: localURI, Host: domain}
	}
	fromParams := sip.NewParams()
	if localTag != "" {
		fromParams.Add("tag", localTag)
	}
	req.AppendHeader(&sip.FromHeader{
		Address: fromUri,
		Params:  fromParams,
	})

	// Add To header (remote party)
	var toUri sip.Uri
	if strings.HasPrefix(remoteURI, "sip:") {
		if err := sip.ParseUri(remoteURI, &toUri); err != nil {
			fmt.Printf("[%s] Failed to parse To URI '%s': %v\n", sess.ID, remoteURI, err)
			toUri = sip.Uri{User: remoteURI, Host: domain}
		}
	} else {
		toUri = sip.Uri{User: remoteURI, Host: domain}
	}
	toParams := sip.NewParams()
	if remoteTag != "" {
		toParams.Add("tag", remoteTag)
	}
	req.AppendHeader(&sip.ToHeader{
		Address: toUri,
		Params:  toParams,
	})

	// Add Call-ID
	req.AppendHeader(sip.NewHeader("Call-ID", sipCallID))

	// Add CSeq
	var cseq int
	if direction == "inbound" {
		cseq = 1
	} else {
		cseq = dialogCSeq + 1
	}
	req.AppendHeader(sip.NewHeader("CSeq", fmt.Sprintf("%d BYE", cseq)))

	// Add Max-Forwards
	req.AppendHeader(sip.NewHeader("Max-Forwards", "70"))

	// Add Contact header
	var contactUser string
	if strings.HasPrefix(localURI, "sip:") {
		var parsedLocalUri sip.Uri
		if err := sip.ParseUri(localURI, &parsedLocalUri); err == nil {
			contactUser = parsedLocalUri.User
		} else {
			contactUser = localURI
		}
	} else {
		contactUser = localURI
	}
	contactUri := sip.Uri{
		User: contactUser,
		Host: s.publicAddress,
		Port: s.sipPort,
	}
	byeContactParams := sip.NewParams()
	byeContactParams.Add("transport", "tcp")
	contactUri.UriParams = byeContactParams
	req.AppendHeader(&sip.ContactHeader{
		Address: contactUri,
	})

	// Add User-Agent
	req.AppendHeader(sip.NewHeader("User-Agent", "TTRS-K2Gateway/1.0"))

	// Set destination
	destinationAddr := fmt.Sprintf("%s:%d", resolvedIP, port)
	req.SetDestination(destinationAddr)

	// CRITICAL: Force TCP transport to prevent sipgo from switching transports
	req.SetTransport("TCP")

	fmt.Printf("[%s] Created BYE request - Call-ID: %s, CSeq: %d\n", sess.ID, sipCallID, cseq)

	// Print full BYE request for debugging
	fmt.Printf("\n=== BYE Request (Session %s) ===\n", sess.ID)
	fmt.Printf("%s\n", req.String())
	fmt.Printf("================================\n\n")

	return req, nil
}

// sendAckForInvite creates and sends an ACK request for a 200 OK response to INVITE
func (s *Server) sendAckForInvite(inviteReq *sip.Request, res *sip.Response) {
	// Use Contact header from response as Request-URI if available
	recipient := inviteReq.Recipient
	if contact := res.Contact(); contact != nil {
		recipient = contact.Address
	}

	// Create ACK request
	ackReq := sip.NewRequest(sip.ACK, recipient)

	// Handle Record-Route headers from response
	recordRoutes := res.GetHeaders("Record-Route")
	for i := len(recordRoutes) - 1; i >= 0; i-- {
		ackReq.AppendHeader(sip.NewHeader("Route", recordRoutes[i].Value()))
	}

	// Copy Via header from INVITE (with new branch)
	inviteVia := inviteReq.Via()
	transport := "TCP"
	if inviteVia != nil {
		transport = inviteVia.Transport
	}

	viaHop := &sip.ViaHeader{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       transport,
		Host:            s.publicAddress,
		Port:            s.sipPort,
	}
	ackViaParams := sip.NewParams()
	ackViaParams.Add("branch", sip.GenerateBranch())
	ackViaParams.Add("rport", "")
	viaHop.Params = ackViaParams
	ackReq.AppendHeader(viaHop)

	// Copy From header from INVITE
	if fromHeader := inviteReq.From(); fromHeader != nil {
		ackReq.AppendHeader(fromHeader)
	}

	// Copy To header from response (includes tag)
	if toHeader := res.To(); toHeader != nil {
		ackReq.AppendHeader(toHeader)
	}

	// Copy Call-ID from INVITE
	if callID := inviteReq.CallID(); callID != nil {
		ackReq.AppendHeader(callID)
	}

	// Set CSeq
	if cseq := res.CSeq(); cseq != nil {
		ackReq.AppendHeader(sip.NewHeader("CSeq", fmt.Sprintf("%d ACK", cseq.SeqNo)))
	} else {
		ackReq.AppendHeader(sip.NewHeader("CSeq", "1 ACK"))
	}

	// Add Max-Forwards
	ackReq.AppendHeader(sip.NewHeader("Max-Forwards", "70"))

	// Set destination same as INVITE
	if dest := inviteReq.Destination(); dest != "" {
		ackReq.SetDestination(dest)
	}

	// CRITICAL: Force transport to match INVITE (prevent transport mismatch)
	ackReq.SetTransport(transport)

	// Send ACK
	if err := s.sipClient.WriteRequest(ackReq); err != nil {
		fmt.Printf("Error sending ACK: %v\n", err)
	} else {
		fmt.Printf("ACK sent successfully (Request-URI: %s)\n", recipient.String())
	}
}

// trySendCancel sends a best-effort SIP CANCEL for an in-progress INVITE.
// Used when the INVITE context times out to free resources on the SIP peer.
func (s *Server) trySendCancel(inviteReq *sip.Request, sess *session.Session) {
	if s.sipClient == nil {
		return
	}

	cancelReq := sip.NewRequest(sip.CANCEL, inviteReq.Recipient)

	// RFC 3261 §9.1: CANCEL must carry same Call-ID, From, To, CSeq number
	if h := inviteReq.CallID(); h != nil {
		cancelReq.AppendHeader(sip.NewHeader("Call-ID", h.Value()))
	}
	if h := inviteReq.From(); h != nil {
		cancelReq.AppendHeader(sip.NewHeader("From", h.Value()))
	}
	if h := inviteReq.To(); h != nil {
		cancelReq.AppendHeader(sip.NewHeader("To", h.Value()))
	}
	cancelReq.AppendHeader(sip.NewHeader("CSeq", "1 CANCEL"))
	cancelReq.AppendHeader(sip.NewHeader("Max-Forwards", "70"))

	if dest := inviteReq.Destination(); dest != "" {
		cancelReq.SetDestination(dest)
	}

	cancelCtx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFn()

	tx, err := s.sipClient.TransactionRequest(cancelCtx, cancelReq)
	if err != nil {
		fmt.Printf("[%s] Failed to send CANCEL: %v\n", sess.ID, err)
		return
	}
	defer tx.Terminate()

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "sip",
		Name:      "sip_cancel_sent",
		SIPCallID: inviteReq.CallID().Value(),
	})

	select {
	case res := <-tx.Responses():
		if res != nil {
			fmt.Printf("[%s] CANCEL response: %d %s\n", sess.ID, res.StatusCode, res.Reason)
		}
	case <-tx.Done():
	case <-cancelCtx.Done():
		fmt.Printf("[%s] CANCEL timed out\n", sess.ID)
	}
}

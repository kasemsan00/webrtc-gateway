package sip

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"webrtc-sip-gateway/internal/logstore"
	"webrtc-sip-gateway/internal/session"
	"webrtc-sip-gateway/internal/telemetry"
)

// SendMessage sends a SIP MESSAGE to a destination
func (s *Server) SendMessage(destination, from, body, contentType string) error {
	started := time.Now()
	ctx, span := telemetry.StartSIPSpan(context.Background(), "MESSAGE")
	err := s.sendMessage(destination, from, body, contentType)
	telemetry.EndSIP(ctx, span, "MESSAGE", started, 0, err)
	return err
}

func (s *Server) sendMessage(destination, from, body, contentType string) error {
	params := sipAuthParams{
		Domain:    s.getActiveDomain(),
		Port:      s.getActivePort(),
		Username:  s.getActiveUsername(),
		Password:  s.getActivePassword(),
		Transport: "tcp",
	}
	return s.sendMessageWithParams("", destination, from, body, contentType, params)
}

// SendMessageForSession sends an out-of-dialog MESSAGE through the PBX using
// identities and credentials owned by sess. Asterisk is a B2BUA, so an
// in-dialog MESSAGE can be accepted on one call leg without being forwarded to
// the other leg. PBX-routed chat must instead address the remote SIP user and
// identify the local agent explicitly.
func (s *Server) SendMessageForSession(sess *session.Session, body, contentType string) error {
	if sess == nil {
		return fmt.Errorf("session is required")
	}
	destination, from, err := sessionMessageParties(sess)
	if err != nil {
		return err
	}
	params, err := s.resolveSessionSIPParams(sess)
	if err != nil {
		return fmt.Errorf("resolve SIP message route: %w", err)
	}
	return s.sendMessageWithParams(sess.ID, destination, from, body, contentType, params)
}

func sessionMessageParties(sess *session.Session) (destination, from string, err error) {
	direction, callFrom, callTo, _ := sess.GetCallInfo()
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "inbound":
		destination = messageURIAddress(callFrom)
		from = messageURIUsername(callTo)
	case "outbound":
		destination = messageURIAddress(callTo)
		from = messageURIUsername(callFrom)
	default:
		return "", "", fmt.Errorf("session %s has unsupported call direction %q", sess.ID, direction)
	}
	if destination == "" {
		return "", "", fmt.Errorf("session %s has no remote SIP address", sess.ID)
	}
	if from == "" {
		_, _, _, _, authUser, _, _ := sess.GetSIPAuthContext()
		from = messageURIUsername(authUser)
	}
	if from == "" {
		return "", "", fmt.Errorf("session %s has no local SIP user", sess.ID)
	}
	return destination, from, nil
}

// messageURIAddress preserves the remote SIP address captured from the active
// call. This matches Linphone's linphone_call_get_remote_address() chat-room
// behavior and, importantly, keeps the call peer's URI domain in Request-URI.
func messageURIAddress(value string) string {
	value = strings.TrimSpace(value)
	if start := strings.Index(value, "<"); start >= 0 {
		if end := strings.Index(value[start+1:], ">"); end >= 0 {
			return strings.TrimSpace(value[start+1 : start+1+end])
		}
	}
	return value
}

func messageURIUsername(value string) string {
	value = messageURIAddress(value)
	uriText := value
	if !strings.HasPrefix(uriText, "sip:") && !strings.HasPrefix(uriText, "sips:") {
		uriText = "sip:" + uriText
	}
	var parsed sip.Uri
	if err := sip.ParseUri(uriText, &parsed); err == nil && parsed.User != "" {
		return strings.TrimSpace(parsed.User)
	}
	return normalizeSIPUser(value)
}

func (s *Server) sendMessageWithParams(sessionID, destination, from, body, contentType string, params sipAuthParams) error {
	if s.sipClient == nil {
		return fmt.Errorf("SIP client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	payloadID := s.storePayload(ctx, &logstore.PayloadRecord{
		SessionID:   sessionID,
		Timestamp:   time.Now(),
		Kind:        "sip_message",
		ContentType: contentType,
		BodyText:    body,
	})

	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sessionID,
		Category:  "sip",
		Name:      "sip_message_send_request",
		PayloadID: payloadID,
		Data:      map[string]interface{}{"destination": destination, "from": from},
	})

	// Use the session/configured username if from is empty.
	if from == "" {
		from = params.Username
	}

	domain := params.Domain
	port := params.Port
	if domain == "" {
		return fmt.Errorf("SIP domain is required")
	}
	if port == 0 {
		port = 5060
	}
	transport := strings.ToUpper(strings.TrimSpace(params.Transport))
	if transport == "" {
		transport = "TCP"
	}

	recipient, err := resolveMessageRecipient(destination, domain, port)
	if err != nil {
		return err
	}

	// Request-URI identifies the active call peer, while the transaction's
	// network next hop is always the session's registered proxy/Asterisk. Do not
	// replace recipient.Host with a resolved IP: that changes the SIP identity
	// Linphone preserves when it creates a chat room from the active call.
	nextHopHost, err := resolveMessageNextHop(domain)
	if err != nil {
		return err
	}

	// Create MESSAGE request
	req := sip.NewRequest(sip.MESSAGE, recipient)
	req.SetBody([]byte(body))

	// Add Via header
	viaHop := &sip.ViaHeader{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       transport,
		Host:            s.publicAddress,
		Port:            s.sipPort,
	}
	viaParams := sip.NewParams()
	viaParams.Add("branch", sip.GenerateBranch())
	viaHop.Params = viaParams
	req.AppendHeader(viaHop)

	// Add From header
	fromUri := sip.Uri{
		User: from,
		Host: domain,
	}
	fromParams := sip.NewParams()
	fromParams.Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(&sip.FromHeader{
		Address: fromUri,
		Params:  fromParams,
	})

	// Add To header (use the same recipient URI)
	req.AppendHeader(&sip.ToHeader{
		Address: recipient,
	})

	// Add Call-ID
	callID := fmt.Sprintf("%s@%s:%d", sip.GenerateTagN(16), s.publicAddress, s.sipPort)
	req.AppendHeader(sip.NewHeader("Call-ID", callID))

	// Add CSeq
	req.AppendHeader(sip.NewHeader("CSeq", "1 MESSAGE"))

	// Add Max-Forwards
	req.AppendHeader(sip.NewHeader("Max-Forwards", "70"))

	// Add Content-Type
	if contentType == "" {
		contentType = "text/plain;charset=UTF-8"
	}
	req.AppendHeader(sip.NewHeader("Content-Type", contentType))

	// Add Contact header (like received MESSAGE: Contact: <sip:00025@203.150.245.41:5060>)
	contactUri := sip.Uri{
		User: from,
		Host: s.publicAddress,
		Port: s.sipPort,
	}
	req.AppendHeader(&sip.ContactHeader{
		Address: contactUri,
	})

	// Add User-Agent
	req.AppendHeader(sip.NewHeader("User-Agent", "WebRTC-SIP-Gateway/1.0"))

	// Route through the configured registrar/proxy, independently of the
	// logical recipient in Request-URI and To.
	destinationAddr := fmt.Sprintf("%s:%d", nextHopHost, port)
	req.SetDestination(destinationAddr)

	// Keep the registered trunk transport stable across authentication retries.
	req.SetTransport(transport)
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sessionID,
		Category:  "sip",
		Name:      "sip_message_route_selected",
		Data: map[string]interface{}{
			"request_uri":       recipient.String(),
			"proxy_destination": destinationAddr,
			"from":              from + "@" + domain,
			"transport":         transport,
		},
	})

	// Debug logging before sending
	if s.config.DebugSIPMessage {
		fmt.Printf("\n=== 💬 Sending SIP MESSAGE ===\n")
		fmt.Printf("Request-URI: %s\n", recipient.String())
		fmt.Printf("Proxy destination: %s\n", destinationAddr)
		fmt.Printf("From: %s\n", from+"@"+domain)
		fmt.Printf("Content-Type: %s\n", contentType)
		fmt.Printf("Body: %s\n", body)
		fmt.Printf("--- Full Request ---\n")
		fmt.Printf("%s\n", req.String())
		fmt.Printf("==============================\n\n")
	} else {
		fmt.Printf("💬 Sending MESSAGE (%d bytes)\n", len(body))
	}

	// Send MESSAGE using TransactionRequest
	tx, err := s.sipClient.TransactionRequest(ctx, req)
	if err != nil {
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sessionID,
			Category:  "sip",
			Name:      "sip_message_send_failed",
			Data:      map[string]interface{}{"error": err.Error()},
		})
		return fmt.Errorf("failed to send MESSAGE: %w", err)
	}
	defer tx.Terminate()

	// Wait for response
	select {
	case res := <-tx.Responses():
		if res != nil {
			if s.config.DebugSIPMessage {
				fmt.Printf("💬 MESSAGE response: %d %s\n", res.StatusCode, res.Reason)
			}
			s.logEvent(&logstore.Event{
				Timestamp:     time.Now(),
				SessionID:     sessionID,
				Category:      "sip",
				Name:          "sip_message_response",
				SIPStatusCode: res.StatusCode,
				Data:          map[string]interface{}{"reason": res.Reason},
			})
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				return nil
			}
			// Handle authentication challenge
			if res.StatusCode == 401 || res.StatusCode == 407 {
				tx.Terminate()
				s.logEvent(&logstore.Event{
					Timestamp:     time.Now(),
					SessionID:     sessionID,
					Category:      "sip",
					Name:          "sip_message_auth_challenge",
					SIPStatusCode: res.StatusCode,
				})
				return s.handleMessageAuthWithParams(ctx, req, res, params)
			}
			return fmt.Errorf("MESSAGE failed: %d %s", res.StatusCode, res.Reason)
		}
	case <-tx.Done():
		if err := tx.Err(); err != nil {
			s.logEvent(&logstore.Event{
				Timestamp: time.Now(),
				SessionID: sessionID,
				Category:  "sip",
				Name:      "sip_message_transaction_error",
				Data:      map[string]interface{}{"error": err.Error()},
			})
			return fmt.Errorf("MESSAGE transaction error: %w", err)
		}
	case <-ctx.Done():
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sessionID,
			Category:  "sip",
			Name:      "sip_message_timeout",
		})
		return fmt.Errorf("MESSAGE timed out")
	}

	return nil
}

func resolveMessageNextHop(domain string) (string, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "", fmt.Errorf("SIP domain is required")
	}
	if net.ParseIP(domain) != nil {
		return domain, nil
	}
	ips, err := net.LookupIP(domain)
	if err != nil {
		return "", fmt.Errorf("failed to resolve SIP proxy %s: %w", domain, err)
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("no IP addresses found for SIP proxy %s", domain)
	}
	return ips[0].String(), nil
}

// resolveMessageRecipient distinguishes a bare SIP username from a hostname.
// sipgo parses "sip:1429900148716" as a host-only URI; for PBX-routed chat the
// same value means user 1429900148716 at the session's SIP domain.
func resolveMessageRecipient(destination, domain string, port int) (sip.Uri, error) {
	destination = messageURIAddress(destination)
	if destination == "" {
		return sip.Uri{}, fmt.Errorf("SIP MESSAGE destination is required")
	}
	if domain == "" {
		return sip.Uri{}, fmt.Errorf("SIP domain is required")
	}
	if port == 0 {
		port = 5060
	}

	hasScheme := strings.HasPrefix(destination, "sip:") || strings.HasPrefix(destination, "sips:")
	if !hasScheme && !strings.Contains(destination, "@") {
		return sip.Uri{User: destination, Host: domain, Port: port}, nil
	}

	uriText := destination
	if !hasScheme {
		uriText = "sip:" + uriText
	}
	var recipient sip.Uri
	if err := sip.ParseUri(uriText, &recipient); err != nil {
		return sip.Uri{}, fmt.Errorf("invalid SIP MESSAGE destination %q: %w", destination, err)
	}
	// Treat an explicit host-less URI such as sip:00025 as a username too.
	if !strings.Contains(strings.TrimPrefix(strings.TrimPrefix(destination, "sip:"), "sips:"), "@") && recipient.User == "" {
		recipient.User = recipient.Host
		recipient.Host = domain
	}
	if recipient.User == "" {
		return sip.Uri{}, fmt.Errorf("SIP MESSAGE destination %q has no user", destination)
	}
	if recipient.Host == "" {
		recipient.Host = domain
	}
	if recipient.Port == 0 {
		recipient.Port = port
	}
	return recipient, nil
}

// handleMessageAuth handles authentication for MESSAGE requests
func (s *Server) handleMessageAuthWithParams(ctx context.Context, originalReq *sip.Request, challenge *sip.Response, params sipAuthParams) error {
	password := params.Password
	if password == "" {
		return fmt.Errorf("authentication required but no password configured")
	}

	if s.config.DebugSIPMessage {
		fmt.Printf("💬 MESSAGE authentication required, retrying with credentials\n")
		for _, header := range challenge.GetHeaders("WWW-Authenticate") {
			fmt.Printf("💬 WWW-Authenticate: %s\n", header.Value())
		}
	}

	// Clone the original request
	authReq := originalReq.Clone()

	// Remove old Via header and add new one with fresh branch
	authReq.RemoveHeader("Via")
	viaHop := &sip.ViaHeader{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       strings.ToUpper(originalReq.Transport()),
		Host:            s.publicAddress,
		Port:            s.sipPort,
	}
	authViaParams := sip.NewParams()
	authViaParams.Add("branch", sip.GenerateBranch())
	viaHop.Params = authViaParams
	authReq.PrependHeader(viaHop)

	// Update CSeq to 2 for the authenticated request
	authReq.RemoveHeader("CSeq")
	authReq.AppendHeader(sip.NewHeader("CSeq", "2 MESSAGE"))

	// Create digest credentials
	digest := sipgo.DigestAuth{
		Username: params.Username,
		Password: password,
	}

	if s.config.DebugSIPMessage {
		fmt.Printf("💬 Sending authenticated MESSAGE with username: %s\n", params.Username)
	}

	// Use DoDigestAuth to send authenticated request
	res, err := s.sipClient.DoDigestAuth(ctx, authReq, challenge, digest)
	if err != nil {
		return fmt.Errorf("authenticated MESSAGE failed: %w", err)
	}

	if s.config.DebugSIPMessage {
		fmt.Printf("💬 Authenticated MESSAGE response: %d %s\n", res.StatusCode, res.Reason)
	}

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}

	return fmt.Errorf("authenticated MESSAGE failed: %d %s", res.StatusCode, res.Reason)
}

// SendMessageToSession sends a SIP MESSAGE within an existing call session (in-dialog)
// This sends directly to the remote Contact address from the session
func (s *Server) SendMessageToSession(sess *session.Session, body, contentType string) error {
	started := time.Now()
	ctx, span := telemetry.StartSIPSpan(context.Background(), "MESSAGE")
	err := s.sendMessageToSession(sess, body, contentType)
	telemetry.EndSIP(ctx, span, "MESSAGE", started, 0, err)
	return err
}

// createInDialogMessageRequest builds MESSAGE from the same dialog metadata as
// BYE and re-INVITE. Request-URI follows the remote Contact, while From/To,
// tags, Call-ID, Route set, and the network next hop remain those of the dialog.
func (s *Server) createInDialogMessageRequest(
	sess *session.Session,
	body string,
	contentType string,
) (*sip.Request, error) {
	if sess == nil {
		return nil, fmt.Errorf("session is required")
	}
	if !sess.HasDialogState() {
		return nil, fmt.Errorf("session has no SIP dialog state")
	}
	_, _, remoteContact, _, _, _, _ := sess.GetSIPDialogState()
	if strings.TrimSpace(remoteContact) == "" {
		return nil, fmt.Errorf("session has no remote contact address")
	}
	_, _, _, sipCallID := sess.GetCallInfo()
	if strings.TrimSpace(sipCallID) == "" {
		return nil, fmt.Errorf("session has no SIP Call-ID")
	}

	req, err := s.createBYERequest(sess)
	if err != nil {
		return nil, fmt.Errorf("build in-dialog MESSAGE: %w", err)
	}
	req.Method = sip.MESSAGE
	req.ReplaceHeader(&sip.CSeqHeader{
		SeqNo:      uint32(sess.NextSIPCSeq()),
		MethodName: sip.MESSAGE,
	})
	if contentType == "" {
		contentType = "text/plain;charset=UTF-8"
	}
	req.AppendHeader(sip.NewHeader("Content-Type", contentType))
	req.SetBody([]byte(body))
	return req, nil
}

func (s *Server) sendMessageToSession(sess *session.Session, body, contentType string) error {
	if s.sipClient == nil {
		return fmt.Errorf("SIP client not initialized")
	}
	if contentType == "" {
		contentType = "text/plain;charset=UTF-8"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, _, _, sipCallID := sess.GetCallInfo()

	payloadID := s.storePayload(ctx, &logstore.PayloadRecord{
		SessionID:   sess.ID,
		Timestamp:   time.Now(),
		Kind:        "sip_message",
		ContentType: contentType,
		BodyText:    body,
	})
	s.logEvent(&logstore.Event{
		Timestamp: time.Now(),
		SessionID: sess.ID,
		Category:  "sip",
		Name:      "sip_message_in_dialog_send_request",
		PayloadID: payloadID,
		Data:      map[string]interface{}{"call_id": sipCallID},
	})
	req, err := s.createInDialogMessageRequest(sess, body, contentType)
	if err != nil {
		return err
	}

	// Debug logging
	if s.config.DebugSIPMessage {
		fmt.Printf("\n=== 💬 Sending In-Dialog SIP MESSAGE ===\n")
		fmt.Printf("Session: %s\n", sess.ID)
		fmt.Printf("Request-URI: %s\n", req.Recipient.String())
		fmt.Printf("Destination: %s\n", req.Destination())
		fmt.Printf("Call-ID: %s\n", sipCallID)
		fmt.Printf("Content-Type: %s\n", contentType)
		fmt.Printf("Body: %s\n", body)
		fmt.Printf("--- Full Request ---\n")
		fmt.Printf("%s\n", req.String())
		fmt.Printf("==============================\n\n")
	} else {
		fmt.Printf("💬 Sending in-dialog MESSAGE (%d bytes)\n", len(body))
	}

	// Send MESSAGE
	tx, err := s.sipClient.TransactionRequest(ctx, req)
	if err != nil {
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sess.ID,
			Category:  "sip",
			Name:      "sip_message_in_dialog_send_failed",
			Data:      map[string]interface{}{"error": err.Error()},
		})
		return fmt.Errorf("failed to send MESSAGE: %w", err)
	}
	defer tx.Terminate()

	// Wait for response
	select {
	case res := <-tx.Responses():
		if res != nil {
			if s.config.DebugSIPMessage {
				fmt.Printf("💬 In-dialog MESSAGE response: %d %s\n", res.StatusCode, res.Reason)
			}
			s.logEvent(&logstore.Event{
				Timestamp:     time.Now(),
				SessionID:     sess.ID,
				Category:      "sip",
				Name:          "sip_message_in_dialog_response",
				SIPStatusCode: res.StatusCode,
				Data:          map[string]interface{}{"reason": res.Reason},
			})
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				return nil
			}
			return fmt.Errorf("MESSAGE failed: %d %s", res.StatusCode, res.Reason)
		}
	case <-tx.Done():
		if err := tx.Err(); err != nil {
			s.logEvent(&logstore.Event{
				Timestamp: time.Now(),
				SessionID: sess.ID,
				Category:  "sip",
				Name:      "sip_message_in_dialog_transaction_error",
				Data:      map[string]interface{}{"error": err.Error()},
			})
			return fmt.Errorf("MESSAGE transaction error: %w", err)
		}
	case <-ctx.Done():
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: sess.ID,
			Category:  "sip",
			Name:      "sip_message_in_dialog_timeout",
		})
		return fmt.Errorf("MESSAGE timed out")
	}

	return nil
}

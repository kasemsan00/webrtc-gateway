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

// SendMessage sends a SIP MESSAGE to a destination
func (s *Server) SendMessage(destination, from, body, contentType string) error {
	if s.sipClient == nil {
		return fmt.Errorf("SIP client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

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
		Name:      "sip_message_send_request",
		PayloadID: payloadID,
		Data:      map[string]interface{}{"destination": destination, "from": from},
	})

	// Use configured username if from is empty
	if from == "" {
		from = s.getActiveUsername()
	}

	domain := s.getActiveDomain()
	port := s.getActivePort()

	// Parse destination - could be username, username@host, or full SIP URI
	var recipient sip.Uri
	// Try to parse as SIP URI (add sip: prefix if missing)
	uriStr := destination
	if !strings.HasPrefix(uriStr, "sip:") && !strings.HasPrefix(uriStr, "sips:") {
		uriStr = "sip:" + uriStr
	}

	var parsedURI sip.Uri
	if err := sip.ParseUri(uriStr, &parsedURI); err == nil {
		// Successfully parsed as URI
		recipient = parsedURI
		// If parsed URI doesn't have a port, use configured port
		if recipient.Port == 0 {
			recipient.Port = port
		}
		// If parsed URI doesn't have a host, use domain
		if recipient.Host == "" {
			recipient.Host = domain
		}
	} else {
		// Failed to parse as URI, treat as username
		recipient = sip.Uri{
			User: destination,
			Host: domain,
			Port: port,
		}
	}

	// If host is not an IP address, resolve it
	if ip := net.ParseIP(recipient.Host); ip == nil {
		// Host is a domain name, resolve it
		ips, err := net.LookupIP(recipient.Host)
		if err != nil {
			return fmt.Errorf("failed to resolve host %s: %w", recipient.Host, err)
		}
		if len(ips) == 0 {
			return fmt.Errorf("no IP addresses found for host %s", recipient.Host)
		}
		recipient.Host = ips[0].String()
	}

	// Create MESSAGE request
	req := sip.NewRequest(sip.MESSAGE, recipient)
	req.SetBody([]byte(body))

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
	req.AppendHeader(sip.NewHeader("User-Agent", "TTRS-K2Gateway/1.0"))

	// Set destination
	destinationAddr := fmt.Sprintf("%s:%d", recipient.Host, recipient.Port)
	req.SetDestination(destinationAddr)

	// CRITICAL: Force TCP transport to prevent sipgo DoDigestAuth from switching to UDP
	req.SetTransport("TCP")

	// Debug logging before sending
	if s.config.DebugSIPMessage {
		fmt.Printf("\n=== 💬 Sending SIP MESSAGE ===\n")
		fmt.Printf("To: %s\n", recipient.String())
		fmt.Printf("From: %s\n", from+"@"+domain)
		fmt.Printf("Content-Type: %s\n", contentType)
		fmt.Printf("Body: %s\n", body)
		fmt.Printf("--- Full Request ---\n")
		fmt.Printf("%s\n", req.String())
		fmt.Printf("==============================\n\n")
	} else {
		fmt.Printf("💬 Sending MESSAGE to %s (%d bytes)\n", recipient.String(), len(body))
	}

	// Send MESSAGE using TransactionRequest
	tx, err := s.sipClient.TransactionRequest(ctx, req)
	if err != nil {
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: "",
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
				SessionID:     "",
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
					SessionID:     "",
					Category:      "sip",
					Name:          "sip_message_auth_challenge",
					SIPStatusCode: res.StatusCode,
				})
				return s.handleMessageAuth(ctx, req, res)
			}
			return fmt.Errorf("MESSAGE failed: %d %s", res.StatusCode, res.Reason)
		}
	case <-tx.Done():
		if err := tx.Err(); err != nil {
			s.logEvent(&logstore.Event{
				Timestamp: time.Now(),
				SessionID: "",
				Category:  "sip",
				Name:      "sip_message_transaction_error",
				Data:      map[string]interface{}{"error": err.Error()},
			})
			return fmt.Errorf("MESSAGE transaction error: %w", err)
		}
	case <-ctx.Done():
		s.logEvent(&logstore.Event{
			Timestamp: time.Now(),
			SessionID: "",
			Category:  "sip",
			Name:      "sip_message_timeout",
		})
		return fmt.Errorf("MESSAGE timed out")
	}

	return nil
}

// handleMessageAuth handles authentication for MESSAGE requests
func (s *Server) handleMessageAuth(ctx context.Context, originalReq *sip.Request, challenge *sip.Response) error {
	password := s.getActivePassword()
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
		Transport:       "TCP",
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
		Username: s.getActiveUsername(),
		Password: password,
	}

	if s.config.DebugSIPMessage {
		fmt.Printf("💬 Sending authenticated MESSAGE with username: %s\n", s.getActiveUsername())
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
	if s.sipClient == nil {
		return fmt.Errorf("SIP client not initialized")
	}

	_, _, remoteContact, _, _, _, _ := sess.GetSIPDialogState()
	if remoteContact == "" {
		return fmt.Errorf("session has no remote contact address")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Parse the remote contact URI
	var recipient sip.Uri
	fromTag, toTag, remoteContact, routeSet, _, _, _ := sess.GetSIPDialogState()
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
	contactStr := remoteContact
	// Remove angle brackets if present: <sip:user@host> -> sip:user@host
	contactStr = strings.TrimPrefix(contactStr, "<")
	contactStr = strings.TrimSuffix(contactStr, ">")

	if err := sip.ParseUri(contactStr, &recipient); err != nil {
		return fmt.Errorf("failed to parse remote contact URI: %w", err)
	}

	// If no port specified, default to 5060
	if recipient.Port == 0 {
		recipient.Port = 5060
	}

	// Increment CSeq for this session
	cseq := sess.NextSIPCSeq()

	// Create MESSAGE request
	req := sip.NewRequest(sip.MESSAGE, recipient)
	req.SetBody([]byte(body))

	// Add Via header
	viaHop := &sip.ViaHeader{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       "TCP",
		Host:            s.publicAddress,
		Port:            s.sipPort,
	}
	inDialogViaParams := sip.NewParams()
	inDialogViaParams.Add("branch", sip.GenerateBranch())
	viaHop.Params = inDialogViaParams
	req.AppendHeader(viaHop)

	// Add Route headers if we have a route set (for proper routing through proxies)
	for _, route := range routeSet {
		req.AppendHeader(sip.NewHeader("Route", route))
	}

	// Add From header with our tag
	fromUri := sip.Uri{
		User: s.getActiveUsername(),
		Host: s.getActiveDomain(),
	}
	fromParams := sip.NewParams()
	fromParams.Add("tag", fromTag)
	req.AppendHeader(&sip.FromHeader{
		Address: fromUri,
		Params:  fromParams,
	})

	// Add To header with remote tag
	toParams := sip.NewParams()
	toParams.Add("tag", toTag)
	req.AppendHeader(&sip.ToHeader{
		Address: recipient,
		Params:  toParams,
	})

	// Add Call-ID from session
	req.AppendHeader(sip.NewHeader("Call-ID", sipCallID))

	// Add CSeq
	req.AppendHeader(sip.NewHeader("CSeq", fmt.Sprintf("%d MESSAGE", cseq)))

	// Add Max-Forwards
	req.AppendHeader(sip.NewHeader("Max-Forwards", "70"))

	// Add Content-Type
	if contentType == "" {
		contentType = "text/plain;charset=UTF-8"
	}
	req.AppendHeader(sip.NewHeader("Content-Type", contentType))

	// Add Contact header
	contactUri := sip.Uri{
		User: s.getActiveUsername(),
		Host: s.publicAddress,
		Port: s.sipPort,
	}
	req.AppendHeader(&sip.ContactHeader{
		Address: contactUri,
	})

	// Add User-Agent
	req.AppendHeader(sip.NewHeader("User-Agent", "TTRS-K2Gateway/1.0"))

	// Set destination - use the host:port from the recipient Contact
	destinationAddr := fmt.Sprintf("%s:%d", recipient.Host, recipient.Port)
	req.SetDestination(destinationAddr)

	// CRITICAL: Force TCP transport to prevent sipgo DoDigestAuth from switching to UDP
	req.SetTransport("TCP")

	// Debug logging
	if s.config.DebugSIPMessage {
		fmt.Printf("\n=== 💬 Sending In-Dialog SIP MESSAGE ===\n")
		fmt.Printf("Session: %s\n", sess.ID)
		fmt.Printf("To: %s\n", recipient.String())
		fmt.Printf("Call-ID: %s\n", sipCallID)
		fmt.Printf("Content-Type: %s\n", contentType)
		fmt.Printf("Body: %s\n", body)
		fmt.Printf("--- Full Request ---\n")
		fmt.Printf("%s\n", req.String())
		fmt.Printf("==============================\n\n")
	} else {
		fmt.Printf("💬 Sending in-dialog MESSAGE to %s (%d bytes)\n", recipient.String(), len(body))
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

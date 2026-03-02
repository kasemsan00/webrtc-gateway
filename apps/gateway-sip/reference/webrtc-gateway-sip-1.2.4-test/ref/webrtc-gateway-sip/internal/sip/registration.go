package sip

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

// RegisterParams holds parameters for creating SIP REGISTER requests
// Used to unify static and dynamic registration code paths
type RegisterParams struct {
	Domain   string
	Username string
	Port     int
}

// DynamicSIPConfig holds runtime SIP credentials for dynamic registration
type DynamicSIPConfig struct {
	Domain   string
	Username string
	Password string
	Port     int
}

// IsRegistered returns the current SIP registration status
func (s *Server) IsRegistered() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isRegistered
}

// GetRegisteredDomain returns the currently registered SIP domain (if any)
func (s *Server) GetRegisteredDomain() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dynamicConfig != nil {
		return s.dynamicConfig.Domain
	}
	return s.config.Domain
}

// getActiveUsername returns the username to use (dynamic or static)
func (s *Server) getActiveUsername() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dynamicConfig != nil {
		return s.dynamicConfig.Username
	}
	return s.config.Username
}

// getActivePassword returns the password to use (dynamic or static)
func (s *Server) getActivePassword() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dynamicConfig != nil {
		return s.dynamicConfig.Password
	}
	return s.config.Password
}

// getActiveDomain returns the domain to use (dynamic or static)
func (s *Server) getActiveDomain() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dynamicConfig != nil {
		return s.dynamicConfig.Domain
	}
	return s.config.Domain
}

// getActivePort returns the port to use (dynamic or static)
func (s *Server) getActivePort() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dynamicConfig != nil && s.dynamicConfig.Port > 0 {
		return s.dynamicConfig.Port
	}
	return s.config.Port
}

// CreateClient creates a SIP client for outbound calls if configured
func (s *Server) CreateClient() error {
	if s.config.Domain == "" || s.config.Username == "" {
		return nil
	}

	sipClient, err := sipgo.NewClient(s.sipUserAgent)
	if err != nil {
		return fmt.Errorf("failed to create SIP client: %w", err)
	}

	s.sipClient = sipClient
	fmt.Printf("SIP Client created for domain: %s\n", s.config.Domain)
	return nil
}

// Register performs SIP registration to the configured SIP server
func (s *Server) Register(ctx context.Context) error {
	if s.sipClient == nil || s.config.Domain == "" || s.config.Username == "" {
		fmt.Println("SIP Registration skipped - not configured")
		return nil
	}

	fmt.Printf("\n=== SIP Registration ===\n")
	fmt.Printf("Registering to: %s:%d\n", s.config.Domain, s.config.Port)
	fmt.Printf("Username: %s\n", s.config.Username)

	// Create REGISTER request using the proper format
	req, err := s.createRegisterRequest()
	if err != nil {
		return fmt.Errorf("failed to create REGISTER request: %w", err)
	}

	// Send REGISTER request using Do() which handles routing
	fmt.Printf("Sending REGISTER request...\n")
	res, err := s.sipClient.Do(ctx, req)
	if err != nil {
		fmt.Printf("Error sending REGISTER: %v\n", err)
		return fmt.Errorf("failed to send REGISTER request: %w", err)
	}
	fmt.Printf("Received response from server\n")

	// Display full response from Asterisk
	logSIPResponse(res, "Response from Asterisk")

	// Handle response
	if res.StatusCode == 200 {
		fmt.Printf("✓ SIP Registration successful (200 OK)\n")
		fmt.Printf("========================\n\n")

		// Start registration refresh in background
		go s.refreshRegistration(ctx)
		return nil
	} else if res.StatusCode == 401 || res.StatusCode == 407 {
		// Handle authentication challenge
		fmt.Printf("Authentication required (%d), attempting with credentials...\n", res.StatusCode)
		return s.registerWithAuth(ctx, req, res)
	}

	return fmt.Errorf("registration failed with status: %d %s", res.StatusCode, res.Reason)
}

// createRegisterRequest creates a SIP REGISTER request using static config
func (s *Server) createRegisterRequest() (*sip.Request, error) {
	return s.createRegisterRequestWithParams(RegisterParams{
		Domain:   s.config.Domain,
		Username: s.config.Username,
		Port:     s.config.Port,
	})
}

// createRegisterRequestWithParams creates a SIP REGISTER request with given parameters
func (s *Server) createRegisterRequestWithParams(params RegisterParams) (*sip.Request, error) {
	recipient := sip.Uri{
		User: params.Username,
		Host: params.Domain,
		Port: params.Port,
	}

	req := sip.NewRequest(sip.REGISTER, recipient)

	// Via header
	viaHop := &sip.ViaHeader{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       "TCP",
		Host:            s.publicAddress,
		Port:            s.sipPort,
		Params:          sip.NewParams().Add("branch", sip.GenerateBranch()),
	}
	req.AppendHeader(viaHop)

	// From header
	fromParams := sip.NewParams().Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(&sip.FromHeader{
		DisplayName: params.Username,
		Address:     recipient,
		Params:      fromParams,
	})

	// To header
	req.AppendHeader(&sip.ToHeader{
		Address: recipient,
	})

	// Call-ID
	callID := fmt.Sprintf("%s@%s", sip.GenerateTagN(16), s.publicAddress)
	req.AppendHeader(sip.NewHeader("Call-ID", callID))

	// CSeq
	req.AppendHeader(sip.NewHeader("CSeq", "1 REGISTER"))

	// Contact
	req.AppendHeader(&sip.ContactHeader{
		Address: sip.Uri{
			User: params.Username,
			Host: s.publicAddress,
			Port: s.sipPort,
		},
	})

	// Expires
	req.AppendHeader(sip.NewHeader("Expires", "3600"))

	// User-Agent
	req.AppendHeader(sip.NewHeader("User-Agent", "K2-Gateway/1.0"))

	// Resolve domain
	ips, err := net.LookupIP(params.Domain)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve domain %s: %w", params.Domain, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP addresses for domain %s", params.Domain)
	}

	destination := fmt.Sprintf("%s:%d", ips[0].String(), params.Port)
	req.SetDestination(destination)
	fmt.Printf("Resolved %s to %s\n", params.Domain, destination)

	return req, nil
}

// registerWithAuth handles SIP registration with digest authentication
func (s *Server) registerWithAuth(ctx context.Context, originalReq *sip.Request, challenge *sip.Response) error {
	if s.config.Password == "" {
		return fmt.Errorf("authentication required but no password configured")
	}

	fmt.Printf("Authentication required, using credentials...\n")

	// Create digest credentials
	digest := sipgo.DigestAuth{
		Username: s.config.Username,
		Password: s.config.Password,
	}

	// Use DoDigestAuth to send authenticated request
	res, err := s.sipClient.DoDigestAuth(ctx, originalReq, challenge, digest)
	if err != nil {
		return fmt.Errorf("failed to send authenticated REGISTER: %w", err)
	}

	// Display authenticated response from Asterisk
	logSIPResponse(res, "Authenticated Response from Asterisk")

	// Handle response
	if res.StatusCode == 200 {
		fmt.Printf("✓ SIP Registration successful with authentication (200 OK)\n")
		fmt.Printf("========================\n\n")

		// Start registration refresh in background
		go s.refreshRegistration(ctx)
		return nil
	}

	return fmt.Errorf("authenticated registration failed: %d %s", res.StatusCode, res.Reason)
}

// refreshRegistration periodically refreshes SIP registration
func (s *Server) refreshRegistration(ctx context.Context) {
	// Refresh every 50 minutes (expires is 60 minutes)
	ticker := time.NewTicker(50 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fmt.Println("Refreshing SIP registration...")
			if err := s.Register(ctx); err != nil {
				fmt.Printf("Warning: Failed to refresh registration: %v\n", err)
			}
		case <-ctx.Done():
			fmt.Println("Stopping registration refresh")
			return
		}
	}
}

// DynamicRegister performs SIP registration with runtime credentials from UI
func (s *Server) DynamicRegister(ctx context.Context, domain, username, password string, port int) error {
	// First unregister if already registered
	if s.IsRegistered() {
		if err := s.DynamicUnregister(ctx); err != nil {
			fmt.Printf("Warning: Failed to unregister before re-registering: %v\n", err)
		}
	}

	// Store dynamic config
	s.mu.Lock()
	s.dynamicConfig = &DynamicSIPConfig{
		Domain:   domain,
		Username: username,
		Password: password,
		Port:     port,
	}
	s.mu.Unlock()

	fmt.Printf("\n=== Dynamic SIP Registration ===\n")
	fmt.Printf("Domain: %s\n", domain)
	fmt.Printf("Username: %s\n", username)
	fmt.Printf("Port: %d\n", port)

	// Ensure SIP client exists
	if s.sipClient == nil {
		sipClient, err := sipgo.NewClient(s.sipUserAgent)
		if err != nil {
			s.mu.Lock()
			s.dynamicConfig = nil
			s.mu.Unlock()
			return fmt.Errorf("failed to create SIP client: %w", err)
		}
		s.sipClient = sipClient
		fmt.Printf("SIP Client created for dynamic registration\n")
	}

	// Create REGISTER request with dynamic credentials
	req, err := s.createDynamicRegisterRequest(domain, username, port)
	if err != nil {
		s.mu.Lock()
		s.dynamicConfig = nil
		s.mu.Unlock()
		return fmt.Errorf("failed to create REGISTER request: %w", err)
	}

	// Send REGISTER request
	fmt.Printf("Sending REGISTER request to %s...\n", domain)
	res, err := s.sipClient.Do(ctx, req)
	if err != nil {
		s.mu.Lock()
		s.dynamicConfig = nil
		s.mu.Unlock()
		return fmt.Errorf("failed to send REGISTER request: %w", err)
	}

	fmt.Printf("Response: %d %s\n", res.StatusCode, res.Reason)

	// Handle authentication challenge
	if res.StatusCode == 401 || res.StatusCode == 407 {
		fmt.Printf("Authentication required (%d), sending credentials...\n", res.StatusCode)

		digest := sipgo.DigestAuth{
			Username: username,
			Password: password,
		}

		res, err = s.sipClient.DoDigestAuth(ctx, req, res, digest)
		if err != nil {
			s.mu.Lock()
			s.dynamicConfig = nil
			s.mu.Unlock()
			return fmt.Errorf("authentication failed: %w", err)
		}
		fmt.Printf("Auth Response: %d %s\n", res.StatusCode, res.Reason)
	}

	if res.StatusCode == 200 {
		fmt.Printf("✅ Dynamic SIP Registration successful!\n")
		fmt.Printf("================================\n\n")

		// Mark as registered
		s.mu.Lock()
		s.isRegistered = true
		s.mu.Unlock()

		// Start registration refresh with cancellable context
		regCtx, cancel := context.WithCancel(context.Background())
		s.mu.Lock()
		s.registrationCancel = cancel
		s.mu.Unlock()
		go s.refreshDynamicRegistration(regCtx)

		return nil
	}

	// Registration failed
	s.mu.Lock()
	s.dynamicConfig = nil
	s.mu.Unlock()
	return fmt.Errorf("registration failed: %d %s", res.StatusCode, res.Reason)
}

// DynamicUnregister unregisters from the SIP server
func (s *Server) DynamicUnregister(ctx context.Context) error {
	if !s.IsRegistered() {
		return nil // Already unregistered
	}

	fmt.Printf("\n=== Dynamic SIP Unregistration ===\n")

	// Stop registration refresh
	s.mu.Lock()
	if s.registrationCancel != nil {
		s.registrationCancel()
		s.registrationCancel = nil
	}
	dynCfg := s.dynamicConfig
	s.mu.Unlock()

	if dynCfg == nil {
		s.mu.Lock()
		s.isRegistered = false
		s.mu.Unlock()
		return nil
	}

	// Send REGISTER with Expires: 0 to unregister
	req, err := s.createDynamicRegisterRequest(dynCfg.Domain, dynCfg.Username, dynCfg.Port)
	if err != nil {
		s.mu.Lock()
		s.isRegistered = false
		s.dynamicConfig = nil
		s.mu.Unlock()
		return fmt.Errorf("failed to create unregister request: %w", err)
	}

	// Replace Expires header with 0
	req.RemoveHeader("Expires")
	req.AppendHeader(sip.NewHeader("Expires", "0"))

	// Send unregister request
	res, err := s.sipClient.Do(ctx, req)
	if err != nil {
		fmt.Printf("Warning: Unregister request failed: %v\n", err)
	} else if res.StatusCode == 401 || res.StatusCode == 407 {
		// Handle auth for unregister
		digest := sipgo.DigestAuth{
			Username: dynCfg.Username,
			Password: dynCfg.Password,
		}
		res, _ = s.sipClient.DoDigestAuth(ctx, req, res, digest)
		if res != nil {
			fmt.Printf("Unregister response: %d %s\n", res.StatusCode, res.Reason)
		}
	} else {
		fmt.Printf("Unregister response: %d %s\n", res.StatusCode, res.Reason)
	}

	// Clear state
	s.mu.Lock()
	s.isRegistered = false
	s.dynamicConfig = nil
	s.mu.Unlock()

	fmt.Printf("✅ Unregistered from SIP server\n")
	fmt.Printf("=================================\n\n")
	return nil
}

// createDynamicRegisterRequest creates a REGISTER request with dynamic credentials
func (s *Server) createDynamicRegisterRequest(domain, username string, port int) (*sip.Request, error) {
	return s.createRegisterRequestWithParams(RegisterParams{
		Domain:   domain,
		Username: username,
		Port:     port,
	})
}

// refreshDynamicRegistration periodically refreshes dynamic SIP registration
func (s *Server) refreshDynamicRegistration(ctx context.Context) {
	ticker := time.NewTicker(50 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.mu.RLock()
			dynCfg := s.dynamicConfig
			s.mu.RUnlock()

			if dynCfg == nil {
				return
			}

			fmt.Println("Refreshing dynamic SIP registration...")
			if err := s.DynamicRegister(context.Background(), dynCfg.Domain, dynCfg.Username, dynCfg.Password, dynCfg.Port); err != nil {
				fmt.Printf("Warning: Failed to refresh registration: %v\n", err)
			}
		case <-ctx.Done():
			fmt.Println("Stopping dynamic registration refresh")
			return
		}
	}
}

// logSIPResponse logs SIP response headers in a consistent format
func logSIPResponse(res *sip.Response, title string) {
	fmt.Printf("\n--- %s ---\n", title)
	fmt.Printf("Status: %d %s\n", res.StatusCode, res.Reason)
	fmt.Printf("Headers:\n")

	headersToLog := []string{"Via", "From", "To", "Call-ID", "CSeq", "Contact", "Expires", "Date", "Server", "WWW-Authenticate"}
	for _, headerName := range headersToLog {
		for _, header := range res.GetHeaders(headerName) {
			fmt.Printf("  %s: %s\n", headerName, header.Value())
		}
	}

	if len(res.Body()) > 0 {
		fmt.Printf("Body: %s\n", string(res.Body()))
	}
	fmt.Printf("--------------------------------------------\n\n")
}

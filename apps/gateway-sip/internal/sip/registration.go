package sip

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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

// registrationState tracks the single refresh goroutine for static registration
type registrationState struct {
	cancel  context.CancelFunc
	started atomic.Bool
	mu      sync.Mutex
}

// getActiveUsername returns the username to use (dynamic or static)
func (s *Server) getActiveUsername() string {
	return s.config.Username
}

// getActivePassword returns the password to use (dynamic or static)
func (s *Server) getActivePassword() string {
	return s.config.Password
}

// getActiveDomain returns the domain to use (dynamic or static)
func (s *Server) getActiveDomain() string {
	return s.config.Domain
}

// getActivePort returns the port to use (dynamic or static)
func (s *Server) getActivePort() int {
	return s.config.Port
}

// CreateClient creates a SIP client for outbound calls if configured
func (s *Server) CreateClient() error {
	if s.sipClient != nil {
		return nil
	}

	sipClient, err := sipgo.NewClient(s.sipUserAgent)
	if err != nil {
		return fmt.Errorf("failed to create SIP client: %w", err)
	}

	s.sipClient = sipClient
	if s.config.Domain != "" {
		fmt.Printf("SIP Client created for domain: %s\n", s.config.Domain)
	} else {
		fmt.Printf("SIP Client created (no static domain configured, will use per-call credentials)\n")
	}
	return nil
}

// Register performs SIP registration to the configured SIP server.
// This function is safe to call multiple times; it ensures only one refresh
// goroutine runs at a time.
func (s *Server) Register(ctx context.Context) error {
	if s.sipClient == nil || s.config.Domain == "" || s.config.Username == "" {
		fmt.Println("SIP Registration skipped - not configured")
		return nil
	}

	// Ensure single refresh goroutine using CAS (Compare-And-Swap)
	if !s.regState.started.CompareAndSwap(false, true) {
		// Already registered and refreshing; just re-register synchronously
		fmt.Printf("\n=== SIP Re-registration ===\n")
		return s.doRegister(ctx)
	}

	fmt.Printf("\n=== SIP Registration ===\n")
	fmt.Printf("Registering to: %s:%d\n", s.config.Domain, s.config.Port)
	fmt.Printf("Username: %s\n", s.config.Username)

	// First-time registration: perform registration and start refresh loop
	if err := s.doRegister(ctx); err != nil {
		s.regState.started.Store(false)
		return err
	}

	// Start background refresh loop (only once)
	refreshCtx, cancel := context.WithCancel(context.Background())
	s.regState.mu.Lock()
	s.regState.cancel = cancel
	s.regState.mu.Unlock()

	go s.refreshRegistration(refreshCtx)
	return nil
}

// doRegister performs the actual REGISTER request (without starting refresh goroutine)
func (s *Server) doRegister(ctx context.Context) error {
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
	}
	viaParams := sip.NewParams()
	viaParams.Add("branch", sip.GenerateBranch())
	viaHop.Params = viaParams
	req.AppendHeader(viaHop)

	// From header
	fromParams := sip.NewParams()
	fromParams.Add("tag", sip.GenerateTagN(16))
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

	// Resolve dialable destination (host:port) using SRV if needed
	destination, err := resolveSIPDestination(params.Domain, params.Port, "tcp")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve destination: %w", err)
	}
	req.SetDestination(destination)
	fmt.Printf("Resolved %s:%d to %s\n", params.Domain, params.Port, destination)

	// CRITICAL: Force TCP transport to prevent sipgo DoDigestAuth from switching to UDP
	// DoDigestAuth removes Via header and re-adds it, which can cause transport to switch
	// to default (UDP) if not explicitly set, leading to "context deadline exceeded" errors
	req.SetTransport("TCP")

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
		return nil
	}

	return fmt.Errorf("authenticated registration failed: %d %s", res.StatusCode, res.Reason)
}

// refreshRegistration periodically refreshes SIP registration.
// It calls doRegister (not Register) to avoid spawning duplicate goroutines.
func (s *Server) refreshRegistration(ctx context.Context) {
	// Refresh every 50 minutes (expires is 60 minutes)
	ticker := time.NewTicker(50 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fmt.Println("Refreshing SIP registration...")
			if err := s.doRegister(ctx); err != nil {
				fmt.Printf("Warning: Failed to refresh registration: %v\n", err)
			}
		case <-ctx.Done():
			fmt.Println("Stopping registration refresh")
			// Reset state so future Register calls can start a new refresh loop
			s.regState.started.Store(false)
			return
		}
	}
}

// StopRegistration stops the background registration refresh goroutine.
// This should be called during graceful shutdown.
func (s *Server) StopRegistration() {
	s.regState.mu.Lock()
	if s.regState.cancel != nil {
		s.regState.cancel()
	}
	s.regState.mu.Unlock()
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

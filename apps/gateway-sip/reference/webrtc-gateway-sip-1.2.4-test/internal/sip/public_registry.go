package sip

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"k2-gateway/internal/config"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

// PublicAccount represents a SIP public (temporary) account
type PublicAccount struct {
	Key                 string // username@domain (hostname) or username@ip:port (IP literal)
	Domain              string
	Port                int
	Username            string
	Password            string
	IsRegistered        bool
	RefCountActiveCalls int // Number of active calls using this account
	LastUsedAt          time.Time
	ExpiresAt           time.Time // Registration expires time
	RefreshCancel       context.CancelFunc
	LastError           string
	mu                  sync.RWMutex
}

// PublicAccountRegistry manages multiple SIP public accounts
type PublicAccountRegistry struct {
	config     config.SIPPublicConfig
	sipUA      *sipgo.UserAgent
	publicAddr string
	sipPort    int
	accounts   map[string]*PublicAccount // key = username@domain (hostname) or username@ip:port (IP literal)
	mu         sync.RWMutex
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewPublicAccountRegistry creates a new public account registry
func NewPublicAccountRegistry(cfg config.SIPPublicConfig, sipUA *sipgo.UserAgent, publicAddr string, sipPort int) *PublicAccountRegistry {
	return &PublicAccountRegistry{
		config:     cfg,
		sipUA:      sipUA,
		publicAddr: publicAddr,
		sipPort:    sipPort,
		accounts:   make(map[string]*PublicAccount),
		stopCh:     make(chan struct{}),
	}
}

// Start starts the cleanup worker
func (r *PublicAccountRegistry) Start() {
	r.wg.Add(1)
	go r.cleanupWorker()
	fmt.Printf("📞 SIP Public Registry started (cleanup interval: %ds, idle TTL: %ds)\n",
		r.config.CleanupIntervalSeconds, r.config.IdleTTLSeconds)
}

// Stop stops all refresh workers and cleanup
func (r *PublicAccountRegistry) Stop() {
	close(r.stopCh)
	r.wg.Wait()

	// Unregister all accounts (best-effort)
	r.mu.Lock()
	accounts := make([]*PublicAccount, 0, len(r.accounts))
	for _, acc := range r.accounts {
		accounts = append(accounts, acc)
	}
	r.mu.Unlock()

	for _, acc := range accounts {
		r.unregisterAccount(acc)
	}
	fmt.Printf("📞 SIP Public Registry stopped\n")
}

// AcquireAndRegister acquires (or creates) a public account and performs SIP REGISTER
// Returns the account key and any error
// port=0 means "not specified" - use domain-only URI (allows DNS SRV resolution)
func (r *PublicAccountRegistry) AcquireAndRegister(ctx context.Context, domain, username, password string, port int) (string, error) {
	// Create account key: include port only for IP literals (not hostnames)
	// This allows DNS SRV resolution for hostnames without explicit port in the key
	accountKey := buildPublicAccountKey(domain, username, port)

	r.mu.Lock()
	if len(r.accounts) >= r.config.MaxAccounts {
		r.mu.Unlock()
		return "", fmt.Errorf("max public accounts reached (%d)", r.config.MaxAccounts)
	}

	acc, exists := r.accounts[accountKey]
	if !exists {
		// Create new account
		acc = &PublicAccount{
			Key:        accountKey,
			Domain:     domain,
			Port:       port, // 0 means "not specified"
			Username:   username,
			Password:   password,
			LastUsedAt: time.Now(),
		}
		r.accounts[accountKey] = acc
		fmt.Printf("📞 [PublicReg] Created new account: %s\n", accountKey)
	}
	r.mu.Unlock()

	// Register if not already registered
	acc.mu.Lock()
	isRegistered := acc.IsRegistered
	acc.mu.Unlock()

	if !isRegistered {
		if err := r.registerAccount(ctx, acc); err != nil {
			return "", fmt.Errorf("failed to register account %s: %w", accountKey, err)
		}
	}

	return accountKey, nil
}

// buildPublicAccountKey creates a unique key for public account lookups.
// For hostnames (non-IP domains): username@domain (port ignored for key, allows DNS SRV)
// For IP literals: username@ip:port (IPv6 uses brackets: username@[ipv6]:port)
func buildPublicAccountKey(domain, username string, port int) string {
	ipAddr := net.ParseIP(domain)
	if ipAddr != nil && port > 0 {
		// IP literal with explicit port
		if ipAddr.To4() == nil {
			// IPv6: use brackets
			return fmt.Sprintf("%s@[%s]:%d", username, domain, port)
		}
		// IPv4
		return fmt.Sprintf("%s@%s:%d", username, domain, port)
	}
	// Hostname (or port=0): omit port from key
	return fmt.Sprintf("%s@%s", username, domain)
}

// IncrementRefCount increments the ref count for an account (call started)
func (r *PublicAccountRegistry) IncrementRefCount(accountKey string) {
	r.mu.RLock()
	acc, exists := r.accounts[accountKey]
	r.mu.RUnlock()

	if !exists {
		return
	}

	acc.mu.Lock()
	acc.RefCountActiveCalls++
	acc.LastUsedAt = time.Now()
	acc.mu.Unlock()
}

// DecrementRefCount decrements the ref count for an account (call ended)
func (r *PublicAccountRegistry) DecrementRefCount(accountKey string) {
	r.mu.RLock()
	acc, exists := r.accounts[accountKey]
	r.mu.RUnlock()

	if !exists {
		return
	}

	acc.mu.Lock()
	if acc.RefCountActiveCalls > 0 {
		acc.RefCountActiveCalls--
	}
	acc.LastUsedAt = time.Now()
	acc.mu.Unlock()
}

// GetAccount returns account by key (for reading credentials)
func (r *PublicAccountRegistry) GetAccount(accountKey string) (*PublicAccount, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	acc, ok := r.accounts[accountKey]
	return acc, ok
}

// registerAccount performs SIP REGISTER for a public account
func (r *PublicAccountRegistry) registerAccount(ctx context.Context, acc *PublicAccount) error {
	fmt.Printf("📞 [PublicReg] Registering account: %s\n", acc.Key)

	// Create SIP client if needed
	sipClient, err := sipgo.NewClient(r.sipUA)
	if err != nil {
		return fmt.Errorf("failed to create SIP client: %w", err)
	}

	// Create REGISTER request with timeout
	regCtx, cancel := context.WithTimeout(ctx, time.Duration(r.config.RegisterTimeoutSeconds)*time.Second)
	defer cancel()

	req, err := r.createRegisterRequest(acc)
	if err != nil {
		return fmt.Errorf("failed to create REGISTER request: %w", err)
	}

	// Send REGISTER
	res, err := sipClient.Do(regCtx, req)
	if err != nil {
		acc.mu.Lock()
		acc.LastError = err.Error()
		acc.mu.Unlock()
		return fmt.Errorf("failed to send REGISTER: %w", err)
	}

	// Handle auth if needed
	// IMPORTANT: Use a fresh timeout context for the digest-auth retry.
	// Using the same regCtx can lead to "context deadline exceeded" if the first
	// unauthenticated REGISTER already consumed most of the timeout budget.
	if res.StatusCode == 401 || res.StatusCode == 407 {
		authCtx, authCancel := context.WithTimeout(ctx, time.Duration(r.config.RegisterTimeoutSeconds)*time.Second)
		defer authCancel()

		digest := sipgo.DigestAuth{
			Username: acc.Username,
			Password: acc.Password,
		}
		res, err = sipClient.DoDigestAuth(authCtx, req, res, digest)
		if err != nil {
			acc.mu.Lock()
			acc.LastError = err.Error()
			acc.mu.Unlock()
			return fmt.Errorf("auth failed: %w", err)
		}
	}

	if res.StatusCode != 200 {
		errMsg := fmt.Sprintf("registration failed: %d %s", res.StatusCode, res.Reason)
		acc.mu.Lock()
		acc.LastError = errMsg
		acc.mu.Unlock()
		return fmt.Errorf("%s", errMsg)
	}

	// Success
	acc.mu.Lock()
	acc.IsRegistered = true
	acc.ExpiresAt = time.Now().Add(time.Duration(r.config.RegisterExpiresSeconds) * time.Second)
	acc.LastError = ""
	acc.mu.Unlock()

	fmt.Printf("✅ [PublicReg] Account registered: %s (expires in %ds)\n", acc.Key, r.config.RegisterExpiresSeconds)

	// Start refresh worker
	refreshCtx, refreshCancel := context.WithCancel(context.Background())
	acc.mu.Lock()
	if acc.RefreshCancel != nil {
		acc.RefreshCancel() // Cancel old refresh if any
	}
	acc.RefreshCancel = refreshCancel
	acc.mu.Unlock()

	go r.refreshWorker(refreshCtx, acc, sipClient)

	return nil
}

// unregisterAccount sends REGISTER with Expires: 0
func (r *PublicAccountRegistry) unregisterAccount(acc *PublicAccount) {
	acc.mu.Lock()
	if acc.RefreshCancel != nil {
		acc.RefreshCancel()
		acc.RefreshCancel = nil
	}
	if !acc.IsRegistered {
		acc.mu.Unlock()
		return
	}
	acc.mu.Unlock()

	fmt.Printf("📞 [PublicReg] Unregistering account: %s\n", acc.Key)

	sipClient, err := sipgo.NewClient(r.sipUA)
	if err != nil {
		fmt.Printf("⚠️  [PublicReg] Failed to create SIP client for unregister: %v\n", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(r.config.RegisterTimeoutSeconds)*time.Second)
	defer cancel()

	req, err := r.createUnregisterRequest(acc)
	if err != nil {
		fmt.Printf("⚠️  [PublicReg] Failed to create unregister request: %v\n", err)
		return
	}

	res, err := sipClient.Do(ctx, req)
	if err != nil {
		fmt.Printf("⚠️  [PublicReg] Unregister failed: %v\n", err)
		return
	}

	// Handle auth if needed (best-effort)
	if res.StatusCode == 401 || res.StatusCode == 407 {
		digest := sipgo.DigestAuth{
			Username: acc.Username,
			Password: acc.Password,
		}
		res, _ = sipClient.DoDigestAuth(ctx, req, res, digest)
	}

	acc.mu.Lock()
	acc.IsRegistered = false
	acc.mu.Unlock()

	fmt.Printf("✅ [PublicReg] Account unregistered: %s\n", acc.Key)
}

// createRegisterRequest creates REGISTER request for public account
// Resolves dialable destination using SRV lookup (for hostnames when port=0) or explicit port
func (r *PublicAccountRegistry) createRegisterRequest(acc *PublicAccount) (*sip.Request, error) {
	recipient := sip.Uri{
		User: acc.Username,
		Host: acc.Domain,
	}
	// Only set port in URI if explicitly specified AND domain is an IP (hostnames should rely on DNS/SRV)
	ipAddr := net.ParseIP(acc.Domain)
	includePort := acc.Port > 0 && ipAddr != nil
	if includePort {
		recipient.Port = acc.Port
	}

	req := sip.NewRequest(sip.REGISTER, recipient)

	// Via
	viaHop := &sip.ViaHeader{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       "TCP",
		Host:            r.publicAddr,
		Port:            r.sipPort,
		Params:          sip.NewParams().Add("branch", sip.GenerateBranch()),
	}
	req.AppendHeader(viaHop)

	// From
	fromParams := sip.NewParams().Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(&sip.FromHeader{
		DisplayName: acc.Username,
		Address:     recipient,
		Params:      fromParams,
	})

	// To
	req.AppendHeader(&sip.ToHeader{
		Address: recipient,
	})

	// Call-ID
	callID := fmt.Sprintf("%s@%s", sip.GenerateTagN(16), r.publicAddr)
	req.AppendHeader(sip.NewHeader("Call-ID", callID))

	// CSeq
	req.AppendHeader(sip.NewHeader("CSeq", "1 REGISTER"))

	// Contact
	req.AppendHeader(&sip.ContactHeader{
		Address: sip.Uri{
			User: acc.Username,
			Host: r.publicAddr,
			Port: r.sipPort,
		},
	})

	// Expires
	req.AppendHeader(sip.NewHeader("Expires", fmt.Sprintf("%d", r.config.RegisterExpiresSeconds)))

	// User-Agent
	req.AppendHeader(sip.NewHeader("User-Agent", "K2-Gateway/1.0"))

	// Resolve dialable destination (host:port) for SetDestination()
	destination, err := resolveSIPDestination(acc.Domain, acc.Port, "tcp")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve destination: %w", err)
	}
	req.SetDestination(destination)

	// CRITICAL: Force TCP transport to prevent sipgo DoDigestAuth from switching to UDP
	// DoDigestAuth removes Via header and re-adds it, which can cause transport to switch
	// to default (UDP) if not explicitly set, leading to "context deadline exceeded" errors
	req.SetTransport("TCP")

	return req, nil
}

// createUnregisterRequest creates REGISTER with Expires: 0
func (r *PublicAccountRegistry) createUnregisterRequest(acc *PublicAccount) (*sip.Request, error) {
	req, err := r.createRegisterRequest(acc)
	if err != nil {
		return nil, err
	}
	// Replace Expires with 0
	req.RemoveHeader("Expires")
	req.AppendHeader(sip.NewHeader("Expires", "0"))
	return req, nil
}

// refreshWorker periodically refreshes registration
func (r *PublicAccountRegistry) refreshWorker(ctx context.Context, acc *PublicAccount, sipClient *sipgo.Client) {
	// Refresh at 80% of expires time
	refreshInterval := time.Duration(float64(r.config.RegisterExpiresSeconds)*0.8) * time.Second
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fmt.Printf("🔄 [PublicReg] Refreshing registration: %s\n", acc.Key)
			regCtx, cancel := context.WithTimeout(context.Background(), time.Duration(r.config.RegisterTimeoutSeconds)*time.Second)
			req, err := r.createRegisterRequest(acc)
			if err != nil {
				fmt.Printf("⚠️  [PublicReg] Refresh create req failed: %v\n", err)
				cancel()
				continue
			}

			res, err := sipClient.Do(regCtx, req)
			cancel()

			if err != nil {
				fmt.Printf("⚠️  [PublicReg] Refresh failed: %v\n", err)
				acc.mu.Lock()
				acc.LastError = err.Error()
				acc.mu.Unlock()
				continue
			}

			// Handle auth if needed
			if res.StatusCode == 401 || res.StatusCode == 407 {
				authCtx, authCancel := context.WithTimeout(context.Background(), time.Duration(r.config.RegisterTimeoutSeconds)*time.Second)
				digest := sipgo.DigestAuth{
					Username: acc.Username,
					Password: acc.Password,
				}
				res, err = sipClient.DoDigestAuth(authCtx, req, res, digest)
				authCancel()
				if err != nil {
					fmt.Printf("⚠️  [PublicReg] Refresh auth failed: %v\n", err)
					acc.mu.Lock()
					acc.LastError = err.Error()
					acc.mu.Unlock()
					continue
				}
			}

			if res.StatusCode == 200 {
				acc.mu.Lock()
				acc.ExpiresAt = time.Now().Add(time.Duration(r.config.RegisterExpiresSeconds) * time.Second)
				acc.LastError = ""
				acc.mu.Unlock()
				fmt.Printf("✅ [PublicReg] Refreshed: %s\n", acc.Key)
			} else {
				fmt.Printf("⚠️  [PublicReg] Refresh failed: %d %s\n", res.StatusCode, res.Reason)
			}

		case <-ctx.Done():
			fmt.Printf("🛑 [PublicReg] Stopping refresh for: %s\n", acc.Key)
			return
		case <-r.stopCh:
			return
		}
	}
}

// cleanupWorker periodically cleans up idle accounts
func (r *PublicAccountRegistry) cleanupWorker() {
	defer r.wg.Done()

	ticker := time.NewTicker(time.Duration(r.config.CleanupIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.cleanup()
		case <-r.stopCh:
			return
		}
	}
}

// cleanup removes idle accounts (refCount==0 && idle > TTL)
func (r *PublicAccountRegistry) cleanup() {
	now := time.Now()
	idleTTL := time.Duration(r.config.IdleTTLSeconds) * time.Second

	r.mu.Lock()
	toRemove := make([]*PublicAccount, 0)
	for key, acc := range r.accounts {
		acc.mu.RLock()
		refCount := acc.RefCountActiveCalls
		lastUsed := acc.LastUsedAt
		acc.mu.RUnlock()

		if refCount == 0 && now.Sub(lastUsed) > idleTTL {
			toRemove = append(toRemove, acc)
			delete(r.accounts, key)
		}
	}
	r.mu.Unlock()

	// Unregister outside lock
	for _, acc := range toRemove {
		r.unregisterAccount(acc)
		fmt.Printf("🗑️  [PublicReg] Cleaned up idle account: %s\n", acc.Key)
	}
}

// resolveSIPDestination resolves a dialable "host:port" destination for SIP.
// Logic:
//   - If domain is an IP literal: return "ip:port" (or "ip:5060" if port=0)
//   - If domain is hostname and port > 0: return "domain:port"
//   - If domain is hostname and port = 0: try SRV lookup, fallback to "domain:5060"
//
// For IPv6 literals, brackets are added: "[ipv6]:port"
func resolveSIPDestination(domain string, port int, transport string) (string, error) {
	ipAddr := net.ParseIP(domain)

	// Case 1: IP literal (IPv4 or IPv6)
	if ipAddr != nil {
		dialPort := port
		if dialPort <= 0 {
			dialPort = 5060 // default SIP port
		}
		if ipAddr.To4() == nil {
			// IPv6: use brackets
			return fmt.Sprintf("[%s]:%d", domain, dialPort), nil
		}
		// IPv4
		return fmt.Sprintf("%s:%d", domain, dialPort), nil
	}

	// Case 2: Hostname with explicit port
	if port > 0 {
		return fmt.Sprintf("%s:%d", domain, port), nil
	}

	// Case 3: Hostname without port - try SRV lookup
	// SRV service name: "_sip._tcp.<domain>" for TCP transport
	serviceName := "sip"
	protoName := strings.ToLower(transport) // normalize to lowercase (net.LookupSRV expects "tcp"/"udp")
	if protoName == "" {
		protoName = "tcp"
	}

	_, addrs, err := net.LookupSRV(serviceName, protoName, domain)
	if err == nil && len(addrs) > 0 {
		// Sort by priority (lower is better), then by weight (higher is better)
		// net.LookupSRV already returns sorted records
		target := addrs[0].Target
		srvPort := addrs[0].Port

		// Trim trailing dot from target (DNS convention)
		target = trimTrailingDot(target)

		fmt.Printf("📞 [SRV] Resolved %s -> %s:%d (priority %d, weight %d)\n",
			domain, target, srvPort, addrs[0].Priority, addrs[0].Weight)
		return fmt.Sprintf("%s:%d", target, srvPort), nil
	}

	// Case 4: SRV lookup failed or no records - fallback to default port
	fmt.Printf("📞 [SRV] No SRV record for %s, using default %s:5060\n", domain, domain)
	return fmt.Sprintf("%s:5060", domain), nil
}

// trimTrailingDot removes trailing dot from DNS names (e.g., "example.com." -> "example.com")
func trimTrailingDot(s string) string {
	if len(s) > 0 && s[len(s)-1] == '.' {
		return s[:len(s)-1]
	}
	return s
}

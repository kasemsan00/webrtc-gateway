package sip

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"k2-gateway/internal/config"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TrunkListParams defines query parameters for listing trunks with pagination, search, and filtering.
type TrunkListParams struct {
	Page          int        // 1-based page number (default 1)
	PageSize      int        // Items per page (default 20, max 100)
	TrunkID       int64      // Filter by exact trunk ID (0 = no filter)
	TrunkPublicID string     // Filter by public trunk ID (UUID string)
	Search        string     // Search by username or name (ILIKE)
	CreatedAfter  *time.Time // Filter trunks created after this time
	CreatedBefore *time.Time // Filter trunks created before this time
	SortBy        string     // Sort field: id, name, domain, created_at, updated_at (default: id)
	SortDir       string     // Sort direction: asc, desc (default: asc)
}

// TrunkListResult contains paginated trunk list results.
type TrunkListResult struct {
	Items    []*Trunk `json:"items"`
	Total    int      `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"pageSize"`
}

var (
	ErrTrunkNotFound   = errors.New("trunk not found")
	ErrTrunkConflict   = errors.New("trunk conflict")
	ErrTrunkValidation = errors.New("trunk validation")
)

// TrunkUpdatePatch defines mutable trunk fields for partial updates.
type TrunkUpdatePatch struct {
	Name      *string
	Domain    *string
	Port      *int
	Username  *string
	Password  *string
	Transport *string
	Enabled   *bool
	IsDefault *bool
	UpdatedBy *string
}

// CreateTrunkPayload defines required fields for creating a new trunk.
type CreateTrunkPayload struct {
	Name      string
	Domain    string
	Port      int
	Username  string
	Password  string
	Transport string
	Enabled   bool
	IsDefault bool
}

// Trunk represents a SIP trunk account from DB
type Trunk struct {
	ID        int64
	PublicID  string
	Name      string
	Domain    string
	Port      int
	Username  string
	Password  string
	Transport string // "udp" or "tcp"
	Enabled   bool
	IsDefault bool

	// Lease fields (managed by TrunkManager)
	LeaseOwner *string
	LeaseUntil *time.Time

	// Registration status
	LastRegisteredAt *time.Time
	LastError        *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// TrunkManager manages SIP trunk registrations with DB-based lease
type TrunkManager struct {
	db         *pgxpool.Pool
	cfg        *config.Config
	userAgent  *sipgo.UserAgent
	sipClient  *sipgo.Client
	instanceID string
	publicIP   string
	localPort  int

	mu             sync.RWMutex
	trunks         map[int64]*Trunk                 // All loaded trunks
	trunkByPublic  map[string]int64                 // public_id -> trunk ID
	ownedLeases    map[int64]bool                   // Trunks we currently own
	registrations  map[int64]*sip.ClientTransaction // Active registration transactions
	refreshWorkers map[int64]chan struct{}          // Stop signals for refresh workers

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

const trunkManagerDBTimeout = 5 * time.Second

func (tm *TrunkManager) dbContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(tm.ctx, trunkManagerDBTimeout)
}

// NewTrunkManager creates a new trunk manager
func NewTrunkManager(db *pgxpool.Pool, cfg *config.Config, userAgent *sipgo.UserAgent, instanceID string) *TrunkManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &TrunkManager{
		db:             db,
		cfg:            cfg,
		userAgent:      userAgent,
		instanceID:     instanceID,
		publicIP:       cfg.SIP.PublicIP,
		localPort:      cfg.SIP.LocalPort,
		trunks:         make(map[int64]*Trunk),
		trunkByPublic:  make(map[string]int64),
		ownedLeases:    make(map[int64]bool),
		registrations:  make(map[int64]*sip.ClientTransaction),
		refreshWorkers: make(map[int64]chan struct{}),
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Start loads trunks, acquires leases, and starts registration workers
func (tm *TrunkManager) Start() error {
	if !tm.cfg.SIPTrunk.Enable {
		fmt.Printf("📞 [TrunkManager] Trunk mode disabled (SIP_TRUNK_ENABLE=false)\n")
		return nil
	}

	if tm.db == nil {
		return fmt.Errorf("database not available for trunk manager")
	}

	// Create SIP client
	sipClient, err := sipgo.NewClient(tm.userAgent)
	if err != nil {
		return fmt.Errorf("failed to create SIP client: %w", err)
	}
	tm.sipClient = sipClient

	// Load trunks from DB
	if err := tm.loadTrunks(); err != nil {
		return fmt.Errorf("failed to load trunks: %w", err)
	}

	// Acquire leases and register
	if err := tm.acquireAndRegisterAll(); err != nil {
		fmt.Printf("⚠️ [TrunkManager] Failed to acquire/register some trunks: %v\n", err)
	}

	// Start lease heartbeat worker
	tm.wg.Add(1)
	go tm.leaseHeartbeatWorker()

	fmt.Printf("✅ [TrunkManager] Started with instance_id=%s\n", tm.instanceID)
	return nil
}

// RefreshTrunks reloads trunks and attempts to acquire/register
func (tm *TrunkManager) RefreshTrunks() error {
	if !tm.cfg.SIPTrunk.Enable {
		return nil
	}
	if tm.db == nil {
		return fmt.Errorf("database not available for trunk manager")
	}
	if err := tm.loadTrunks(); err != nil {
		return fmt.Errorf("failed to load trunks: %w", err)
	}
	if err := tm.acquireAndRegisterAll(); err != nil {
		return fmt.Errorf("failed to acquire/register trunks: %w", err)
	}
	return nil
}

// Stop releases all leases and stops workers
func (tm *TrunkManager) Stop() {
	if !tm.cfg.SIPTrunk.Enable {
		return
	}

	fmt.Printf("📞 [TrunkManager] Stopping...\n")

	// Cancel context to stop workers
	tm.cancel()

	// Unregister all owned trunks (best-effort)
	tm.mu.RLock()
	ownedIDs := make([]int64, 0, len(tm.ownedLeases))
	for id := range tm.ownedLeases {
		ownedIDs = append(ownedIDs, id)
	}
	tm.mu.RUnlock()

	for _, id := range ownedIDs {
		tm.unregisterTrunk(id)
		tm.releaseLease(id)
	}

	// Wait for workers to finish
	tm.wg.Wait()

	fmt.Printf("✅ [TrunkManager] Stopped\n")
}

// loadTrunks loads enabled trunks from database
func (tm *TrunkManager) loadTrunks() error {
	ctx, cancel := tm.dbContext()
	defer cancel()

	rows, err := tm.db.Query(ctx, `
		SELECT id, public_id, name, domain, port, username, password, transport, enabled, is_default,
		       lease_owner, lease_until, last_registered_at, last_error, created_at, updated_at
		FROM sip_trunks
		WHERE enabled = true
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.trunks = make(map[int64]*Trunk)
	tm.trunkByPublic = make(map[string]int64)

	count := 0
	for rows.Next() {
		trunk := &Trunk{}
		err := rows.Scan(
			&trunk.ID, &trunk.PublicID, &trunk.Name, &trunk.Domain, &trunk.Port,
			&trunk.Username, &trunk.Password, &trunk.Transport,
			&trunk.Enabled, &trunk.IsDefault,
			&trunk.LeaseOwner, &trunk.LeaseUntil,
			&trunk.LastRegisteredAt, &trunk.LastError,
			&trunk.CreatedAt, &trunk.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}
		if trunk.PublicID != "" {
			tm.trunkByPublic[trunk.PublicID] = trunk.ID
		}

		tm.trunks[trunk.ID] = trunk
		count++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows iteration failed: %w", err)
	}

	fmt.Printf("📞 [TrunkManager] Loaded %d enabled trunk(s) from DB\n", count)
	return nil
}

// acquireAndRegisterAll tries to acquire leases and register all loaded trunks
func (tm *TrunkManager) acquireAndRegisterAll() error {
	tm.mu.RLock()
	trunkIDs := make([]int64, 0, len(tm.trunks))
	for id := range tm.trunks {
		trunkIDs = append(trunkIDs, id)
	}
	tm.mu.RUnlock()

	var firstErr error
	for _, id := range trunkIDs {
		if err := tm.acquireLease(id); err != nil {
			fmt.Printf("⚠️ [TrunkManager] Failed to acquire lease for trunk %d: %v\n", id, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		if err := tm.registerTrunk(id); err != nil {
			fmt.Printf("⚠️ [TrunkManager] Failed to register trunk %d: %v\n", id, err)
			if firstErr == nil {
				firstErr = err
			}
			// Keep the lease, will retry in heartbeat
		}
	}

	return firstErr
}

// acquireLease attempts to acquire or renew lease for a trunk using atomic SQL update
func (tm *TrunkManager) acquireLease(trunkID int64) error {
	leaseTTL := time.Duration(tm.cfg.SIPTrunk.LeaseTTLSeconds) * time.Second
	leaseUntil := time.Now().Add(leaseTTL)

	// Atomic lease acquisition:
	// Update if lease is expired OR owned by us OR not owned at all
	ctx, cancel := tm.dbContext()
	defer cancel()

	result, err := tm.db.Exec(ctx, `
		UPDATE sip_trunks
		SET lease_owner = $1, lease_until = $2, updated_at = NOW()
		WHERE id = $3
		  AND enabled = true
		  AND (lease_until IS NULL OR lease_until < NOW() OR lease_owner = $1)
	`, tm.instanceID, leaseUntil, trunkID)

	if err != nil {
		return fmt.Errorf("lease update failed: %w", err)
	}

	if result.RowsAffected() == 0 {
		// Another instance owns the lease
		tm.mu.Lock()
		delete(tm.ownedLeases, trunkID)
		tm.mu.Unlock()
		return fmt.Errorf("trunk %d lease held by another instance", trunkID)
	}

	// Successfully acquired/renewed lease
	tm.mu.Lock()
	tm.ownedLeases[trunkID] = true
	if trunk, ok := tm.trunks[trunkID]; ok {
		lo := tm.instanceID
		trunk.LeaseOwner = &lo
		trunk.LeaseUntil = &leaseUntil
	}
	tm.mu.Unlock()

	fmt.Printf("📞 [TrunkManager] Acquired lease for trunk %d (until %s)\n", trunkID, leaseUntil.Format(time.RFC3339))
	return nil
}

// releaseLease releases the lease for a trunk (best-effort)
func (tm *TrunkManager) releaseLease(trunkID int64) {
	ctx, cancel := tm.dbContext()
	defer cancel()

	_, err := tm.db.Exec(ctx, `
		UPDATE sip_trunks
		SET lease_owner = NULL, lease_until = NULL, updated_at = NOW()
		WHERE id = $1 AND lease_owner = $2
	`, trunkID, tm.instanceID)

	if err != nil {
		fmt.Printf("⚠️ [TrunkManager] Failed to release lease for trunk %d: %v\n", trunkID, err)
	} else {
		fmt.Printf("📞 [TrunkManager] Released lease for trunk %d\n", trunkID)
	}

	tm.mu.Lock()
	delete(tm.ownedLeases, trunkID)
	tm.mu.Unlock()
}

// registerTrunk performs SIP REGISTER for a trunk
func (tm *TrunkManager) registerTrunk(trunkID int64) error {
	tm.mu.RLock()
	trunk, ok := tm.trunks[trunkID]
	tm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("trunk %d not found", trunkID)
	}

	// Build REGISTER request (matching registration.go pattern)
	recipient := sip.Uri{
		User: trunk.Username,
		Host: trunk.Domain,
		Port: trunk.Port,
	}

	req := sip.NewRequest(sip.REGISTER, recipient)

	// Via header
	viaHop := &sip.ViaHeader{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       trunk.Transport,
		Host:            tm.publicIP,
		Port:            tm.localPort,
	}
	viaParams := sip.NewParams()
	viaParams.Add("branch", sip.GenerateBranch())
	viaHop.Params = viaParams
	req.AppendHeader(viaHop)

	// From header
	fromParams := sip.NewParams()
	fromParams.Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(&sip.FromHeader{
		DisplayName: trunk.Username,
		Address:     recipient,
		Params:      fromParams,
	})

	// To header
	req.AppendHeader(&sip.ToHeader{
		Address: recipient,
	})

	// Call-ID
	callID := fmt.Sprintf("%s@%s", sip.GenerateTagN(16), tm.publicIP)
	req.AppendHeader(sip.NewHeader("Call-ID", callID))

	// CSeq
	req.AppendHeader(sip.NewHeader("CSeq", "1 REGISTER"))

	// Contact
	req.AppendHeader(&sip.ContactHeader{
		Address: sip.Uri{
			User: trunk.Username,
			Host: tm.publicIP,
			Port: tm.localPort,
		},
	})

	// Expires
	expires := tm.cfg.SIPPublic.RegisterExpiresSeconds
	req.AppendHeader(sip.NewHeader("Expires", fmt.Sprintf("%d", expires)))

	// User-Agent
	req.AppendHeader(sip.NewHeader("User-Agent", "K2-Gateway-Trunk/1.0"))

	// Resolve dialable destination (host:port) for SetDestination()
	destination, err := resolveSIPDestination(trunk.Domain, trunk.Port, trunk.Transport)
	if err != nil {
		return fmt.Errorf("failed to resolve destination: %w", err)
	}
	req.SetDestination(destination)

	// CRITICAL: Force transport to prevent sipgo DoDigestAuth from switching transports
	// DoDigestAuth removes Via header and re-adds it, which can cause transport to switch
	// to default (UDP) if not explicitly set, leading to "context deadline exceeded" errors
	req.SetTransport(trunk.Transport)

	// Send REGISTER with timeout
	ctx, cancel := context.WithTimeout(tm.ctx, time.Duration(tm.cfg.SIPTrunk.RegisterTimeout)*time.Second)
	defer cancel()

	res, err := tm.sipClient.Do(ctx, req)
	if err != nil {
		tm.updateRegistrationError(trunkID, err.Error())
		return fmt.Errorf("REGISTER request failed: %w", err)
	}

	// Handle 401/407 authentication
	if res.StatusCode == 401 || res.StatusCode == 407 {
		fmt.Printf("📞 [TrunkManager] Trunk %d: Received %d, retrying with auth\n", trunkID, res.StatusCode)

		digest := sipgo.DigestAuth{
			Username: trunk.Username,
			Password: trunk.Password,
		}

		// Resend with credentials
		ctx2, cancel2 := context.WithTimeout(tm.ctx, time.Duration(tm.cfg.SIPTrunk.RegisterTimeout)*time.Second)
		defer cancel2()

		res, err = tm.sipClient.DoDigestAuth(ctx2, req, res, digest)
		if err != nil {
			tm.updateRegistrationError(trunkID, err.Error())
			return fmt.Errorf("REGISTER auth response timeout: %w", err)
		}
	}

	// Check final response
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		errMsg := fmt.Sprintf("REGISTER failed: %d %s", res.StatusCode, res.Reason)
		tm.updateRegistrationError(trunkID, errMsg)
		return fmt.Errorf("%s", errMsg)
	}

	// Success
	tm.updateRegistrationSuccess(trunkID)

	// Start refresh worker if not already running
	tm.startRefreshWorker(trunkID, expires)

	fmt.Printf("✅ [TrunkManager] Trunk %d registered successfully (expires in %ds)\n", trunkID, expires)
	return nil
}

// unregisterTrunk sends REGISTER with Expires:0 (best-effort)
func (tm *TrunkManager) unregisterTrunk(trunkID int64) {
	tm.mu.RLock()
	trunk, ok := tm.trunks[trunkID]
	stopCh, hasWorker := tm.refreshWorkers[trunkID]
	tm.mu.RUnlock()

	if !ok {
		return
	}

	// Stop refresh worker
	if hasWorker {
		close(stopCh)
		tm.mu.Lock()
		delete(tm.refreshWorkers, trunkID)
		tm.mu.Unlock()
	}

	if err := tm.sendUnregister(trunk); err != nil {
		fmt.Printf("⚠️ [TrunkManager] Trunk %d unregister failed (ignored): %v\n", trunkID, err)
	} else {
		fmt.Printf("📞 [TrunkManager] Trunk %d unregistered\n", trunkID)
	}

	tm.mu.Lock()
	delete(tm.registrations, trunkID)
	tm.mu.Unlock()
}

func (tm *TrunkManager) sendUnregister(trunk *Trunk) error {
	if tm.sipClient == nil {
		return fmt.Errorf("SIP client not initialized")
	}

	// Build REGISTER with Expires:0
	recipient := sip.Uri{
		User: trunk.Username,
		Host: trunk.Domain,
		Port: trunk.Port,
	}

	req := sip.NewRequest(sip.REGISTER, recipient)

	// Via
	viaHop := &sip.ViaHeader{
		ProtocolName:    "SIP",
		ProtocolVersion: "2.0",
		Transport:       trunk.Transport,
		Host:            tm.publicIP,
		Port:            tm.localPort,
	}
	viaParams := sip.NewParams()
	viaParams.Add("branch", sip.GenerateBranch())
	viaHop.Params = viaParams
	req.AppendHeader(viaHop)

	// From
	fromParams := sip.NewParams()
	fromParams.Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(&sip.FromHeader{
		DisplayName: trunk.Username,
		Address:     recipient,
		Params:      fromParams,
	})

	// To
	req.AppendHeader(&sip.ToHeader{
		Address: recipient,
	})

	// Call-ID, CSeq, Contact
	callID := fmt.Sprintf("%s@%s", sip.GenerateTagN(16), tm.publicIP)
	req.AppendHeader(sip.NewHeader("Call-ID", callID))
	req.AppendHeader(sip.NewHeader("CSeq", "1 REGISTER"))
	req.AppendHeader(&sip.ContactHeader{
		Address: sip.Uri{
			User: trunk.Username,
			Host: tm.publicIP,
			Port: tm.localPort,
		},
	})

	// Expires:0
	req.AppendHeader(sip.NewHeader("Expires", "0"))

	// Resolve dialable destination (host:port) for SetDestination()
	destination, err := resolveSIPDestination(trunk.Domain, trunk.Port, trunk.Transport)
	if err != nil {
		return fmt.Errorf("failed to resolve destination: %w", err)
	}
	req.SetDestination(destination)

	// CRITICAL: Force transport to prevent sipgo DoDigestAuth from switching transports
	req.SetTransport(trunk.Transport)

	// Send (short timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := tm.sipClient.Do(ctx, req)
	if err != nil {
		return fmt.Errorf("unregister request failed: %w", err)
	}

	// Handle 401/407 authentication challenge the same way as REGISTER.
	if res.StatusCode == 401 || res.StatusCode == 407 {
		digest := sipgo.DigestAuth{
			Username: trunk.Username,
			Password: trunk.Password,
		}

		authCtx, authCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer authCancel()

		res, err = tm.sipClient.DoDigestAuth(authCtx, req, res, digest)
		if err != nil {
			return fmt.Errorf("unregister auth response failed: %w", err)
		}
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("unregister failed: %d %s", res.StatusCode, res.Reason)
	}
	return nil
}

// startRefreshWorker starts a goroutine to refresh registration before expiry
func (tm *TrunkManager) startRefreshWorker(trunkID int64, expiresSeconds int) {
	tm.mu.Lock()
	// Stop existing worker if any
	if stopCh, exists := tm.refreshWorkers[trunkID]; exists {
		close(stopCh)
	}

	stopCh := make(chan struct{})
	tm.refreshWorkers[trunkID] = stopCh
	tm.mu.Unlock()

	tm.wg.Add(1)
	go func() {
		defer tm.wg.Done()

		// Refresh at 80% of expires time
		refreshInterval := time.Duration(float64(expiresSeconds)*0.8) * time.Second
		ticker := time.NewTicker(refreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Check if we still own the lease
				tm.mu.RLock()
				owned := tm.ownedLeases[trunkID]
				tm.mu.RUnlock()

				if !owned {
					fmt.Printf("📞 [TrunkManager] Trunk %d: Lost lease, stopping refresh worker\n", trunkID)
					return
				}

				// Re-register
				if err := tm.registerTrunk(trunkID); err != nil {
					fmt.Printf("⚠️ [TrunkManager] Trunk %d: Refresh REGISTER failed: %v\n", trunkID, err)
				}

			case <-stopCh:
				fmt.Printf("📞 [TrunkManager] Trunk %d: Refresh worker stopped\n", trunkID)
				return

			case <-tm.ctx.Done():
				return
			}
		}
	}()
}

// leaseHeartbeatWorker periodically renews leases for owned trunks
func (tm *TrunkManager) leaseHeartbeatWorker() {
	defer tm.wg.Done()

	renewInterval := time.Duration(tm.cfg.SIPTrunk.LeaseRenewInterval) * time.Second
	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			tm.mu.RLock()
			ownedIDs := make([]int64, 0, len(tm.ownedLeases))
			for id := range tm.ownedLeases {
				ownedIDs = append(ownedIDs, id)
			}
			tm.mu.RUnlock()

			for _, id := range ownedIDs {
				if err := tm.acquireLease(id); err != nil {
					fmt.Printf("⚠️ [TrunkManager] Failed to renew lease for trunk %d: %v\n", id, err)
					// Lost lease - stop refresh worker
					tm.mu.Lock()
					if stopCh, exists := tm.refreshWorkers[id]; exists {
						close(stopCh)
						delete(tm.refreshWorkers, id)
					}
					delete(tm.registrations, id)
					tm.mu.Unlock()
				}
			}

		case <-tm.ctx.Done():
			fmt.Printf("📞 [TrunkManager] Lease heartbeat worker stopped\n")
			return
		}
	}
}

// updateRegistrationSuccess updates DB with successful registration timestamp
func (tm *TrunkManager) updateRegistrationSuccess(trunkID int64) {
	now := time.Now()
	ctx, cancel := tm.dbContext()
	defer cancel()

	_, err := tm.db.Exec(ctx, `
		UPDATE sip_trunks
		SET last_registered_at = $1, last_error = NULL, updated_at = NOW()
		WHERE id = $2
	`, now, trunkID)

	if err != nil {
		fmt.Printf("⚠️ [TrunkManager] Failed to update registration timestamp for trunk %d: %v\n", trunkID, err)
	}

	tm.mu.Lock()
	if trunk, ok := tm.trunks[trunkID]; ok {
		trunk.LastRegisteredAt = &now
		trunk.LastError = nil
	}
	tm.mu.Unlock()
}

// updateRegistrationError updates DB with registration error
func (tm *TrunkManager) updateRegistrationError(trunkID int64, errMsg string) {
	ctx, cancel := tm.dbContext()
	defer cancel()

	_, err := tm.db.Exec(ctx, `
		UPDATE sip_trunks
		SET last_error = $1, updated_at = NOW()
		WHERE id = $2
	`, errMsg, trunkID)

	if err != nil {
		fmt.Printf("⚠️ [TrunkManager] Failed to update error for trunk %d: %v\n", trunkID, err)
	}

	tm.mu.Lock()
	if trunk, ok := tm.trunks[trunkID]; ok {
		trunk.LastError = &errMsg
	}
	tm.mu.Unlock()
}

// GetTrunkByID returns a trunk by ID (if loaded and enabled)
func (tm *TrunkManager) GetTrunkByID(id int64) (interface{}, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	trunk, ok := tm.trunks[id]
	return trunk, ok
}

// GetTrunkByPublicID returns a trunk by public ID (if loaded and enabled).
func (tm *TrunkManager) GetTrunkByPublicID(publicID string) (interface{}, bool) {
	publicID, ok := NormalizeTrunkPublicID(publicID)
	if !ok {
		return nil, false
	}
	trunkID, ok := tm.GetTrunkIDByPublicID(publicID)
	if !ok {
		return nil, false
	}
	return tm.GetTrunkByID(trunkID)
}

// GetTrunkIDByPublicID resolves trunk ID from public ID.
func (tm *TrunkManager) GetTrunkIDByPublicID(publicID string) (int64, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	trunkID, ok := tm.trunkByPublic[publicID]
	return trunkID, ok
}

// ListOwnedTrunks returns all trunks currently owned by this instance
func (tm *TrunkManager) ListOwnedTrunks() []*Trunk {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	trunks := make([]*Trunk, 0, len(tm.trunks))
	for _, trunk := range tm.trunks {
		trunks = append(trunks, trunk)
	}
	return trunks
}

// GetDefaultTrunk returns the default trunk (if any)
func (tm *TrunkManager) GetDefaultTrunk() (interface{}, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, trunk := range tm.trunks {
		if trunk.IsDefault {
			return trunk, true
		}
	}
	return nil, false
}

// MatchTrunkFromInvite matches an incoming INVITE to a trunk based on Request-URI or To header
// Returns (trunk, owned) where owned indicates if this instance owns the trunk's lease
func (tm *TrunkManager) MatchTrunkFromInvite(req *sip.Request) (*Trunk, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// Extract domain from Request-URI
	ruri := req.Recipient
	if ruri.Host == "" {
		return nil, false
	}

	domain := ruri.Host
	port := ruri.Port
	if port == 0 {
		port = 5060 // Default SIP port
	}

	// Match trunk by domain:port
	for _, trunk := range tm.trunks {
		if trunk.Domain == domain && trunk.Port == port {
			owned := tm.ownedLeases[trunk.ID]
			return trunk, owned
		}
	}

	return nil, false
}

// IsOwnedTrunk checks if this instance owns the lease for a trunk
func (tm *TrunkManager) IsOwnedTrunk(trunkID int64) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.ownedLeases[trunkID]
}

// GetAllOwnedTrunks returns all trunks currently owned by this instance
func (tm *TrunkManager) GetAllOwnedTrunks() []*Trunk {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	owned := make([]*Trunk, 0, len(tm.ownedLeases))
	for id := range tm.ownedLeases {
		if trunk, ok := tm.trunks[id]; ok {
			owned = append(owned, trunk)
		}
	}
	return owned
}

// ListTrunks returns trunks from the database with pagination, search, and time filtering.
func (tm *TrunkManager) ListTrunks(ctx context.Context, params TrunkListParams) (*TrunkListResult, error) {
	if tm.db == nil {
		return nil, fmt.Errorf("database not available for trunk manager")
	}

	// Normalise pagination defaults
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}

	// Build dynamic WHERE clause
	where := "WHERE 1=1"
	args := []interface{}{}
	argIdx := 1

	if params.TrunkID > 0 {
		where += fmt.Sprintf(" AND id = $%d", argIdx)
		args = append(args, params.TrunkID)
		argIdx++
	} else if params.TrunkPublicID != "" {
		publicID, ok := NormalizeTrunkPublicID(params.TrunkPublicID)
		if !ok {
			return &TrunkListResult{
				Items:    []*Trunk{},
				Total:    0,
				Page:     params.Page,
				PageSize: params.PageSize,
			}, nil
		}
		where += fmt.Sprintf(" AND public_id = $%d", argIdx)
		args = append(args, publicID)
		argIdx++
	}
	if params.Search != "" {
		where += fmt.Sprintf(" AND (username ILIKE $%d OR name ILIKE $%d)", argIdx, argIdx+1)
		like := "%" + params.Search + "%"
		args = append(args, like, like)
		argIdx += 2
	}
	if params.CreatedAfter != nil {
		where += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, *params.CreatedAfter)
		argIdx++
	}
	if params.CreatedBefore != nil {
		where += fmt.Sprintf(" AND created_at <= $%d", argIdx)
		args = append(args, *params.CreatedBefore)
		argIdx++
	}

	// Count total matching rows
	countSQL := "SELECT COUNT(*) FROM sip_trunks " + where
	var total int
	if err := tm.db.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count query failed: %w", err)
	}

	// Build ORDER BY clause
	sortColumn := "id"
	switch params.SortBy {
	case "name":
		sortColumn = "name"
	case "domain":
		sortColumn = "domain"
	case "created_at":
		sortColumn = "created_at"
	case "updated_at":
		sortColumn = "updated_at"
	case "port":
		sortColumn = "port"
	case "username":
		sortColumn = "username"
	case "enabled":
		sortColumn = "enabled"
	}
	sortDirection := "ASC"
	if strings.ToLower(params.SortDir) == "desc" {
		sortDirection = "DESC"
	}

	// Fetch page
	offset := (params.Page - 1) * params.PageSize
	dataSQL := fmt.Sprintf(`
		SELECT id, public_id, name, domain, port, username, password, transport, enabled, is_default,
		       lease_owner, lease_until, last_registered_at, last_error, created_at, updated_at
		FROM sip_trunks
		%s
		ORDER BY %s %s
		LIMIT $%d OFFSET $%d
	`, where, sortColumn, sortDirection, argIdx, argIdx+1)
	args = append(args, params.PageSize, offset)

	rows, err := tm.db.Query(ctx, dataSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	trunks := make([]*Trunk, 0)
	for rows.Next() {
		trunk := &Trunk{}
		err := rows.Scan(
			&trunk.ID, &trunk.PublicID, &trunk.Name, &trunk.Domain, &trunk.Port,
			&trunk.Username, &trunk.Password, &trunk.Transport,
			&trunk.Enabled, &trunk.IsDefault,
			&trunk.LeaseOwner, &trunk.LeaseUntil,
			&trunk.LastRegisteredAt, &trunk.LastError,
			&trunk.CreatedAt, &trunk.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		trunks = append(trunks, trunk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	return &TrunkListResult{
		Items:    trunks,
		Total:    total,
		Page:     params.Page,
		PageSize: params.PageSize,
	}, nil
}

// CreateTrunk inserts a new trunk into the database and returns the created trunk.
func (tm *TrunkManager) CreateTrunk(ctx context.Context, payload CreateTrunkPayload) (*Trunk, error) {
	if tm.db == nil {
		return nil, fmt.Errorf("database not available for trunk manager")
	}

	// Validate required fields.
	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrTrunkValidation)
	}
	payload.Domain = strings.TrimSpace(payload.Domain)
	if payload.Domain == "" {
		return nil, fmt.Errorf("%w: domain is required", ErrTrunkValidation)
	}
	payload.Username = strings.TrimSpace(payload.Username)
	if payload.Username == "" {
		return nil, fmt.Errorf("%w: username is required", ErrTrunkValidation)
	}
	payload.Password = strings.TrimSpace(payload.Password)
	if payload.Password == "" {
		return nil, fmt.Errorf("%w: password is required", ErrTrunkValidation)
	}
	if payload.Port < 1 || payload.Port > 65535 {
		payload.Port = 5060
	}
	payload.Transport = strings.ToLower(strings.TrimSpace(payload.Transport))
	if payload.Transport != "tcp" && payload.Transport != "udp" {
		payload.Transport = "tcp"
	}

	tx, err := tm.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin transaction failed: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// If setting as default, clear other defaults.
	if payload.IsDefault {
		if _, err := tx.Exec(ctx, `
			UPDATE sip_trunks
			SET is_default = false, updated_at = NOW()
			WHERE is_default = true
		`); err != nil {
			return nil, fmt.Errorf("clear default trunk failed: %w", err)
		}
	}

	created := &Trunk{}
	publicID := uuid.NewString()
	err = tx.QueryRow(ctx, `
		INSERT INTO sip_trunks (public_id, name, domain, port, username, password, transport, enabled, is_default)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, public_id, name, domain, port, username, password, transport, enabled, is_default,
		          lease_owner, lease_until, last_registered_at, last_error, created_at, updated_at
	`, publicID, payload.Name, payload.Domain, payload.Port, payload.Username, payload.Password,
		payload.Transport, payload.Enabled, payload.IsDefault,
	).Scan(
		&created.ID, &created.PublicID, &created.Name, &created.Domain, &created.Port,
		&created.Username, &created.Password, &created.Transport,
		&created.Enabled, &created.IsDefault,
		&created.LeaseOwner, &created.LeaseUntil,
		&created.LastRegisteredAt, &created.LastError,
		&created.CreatedAt, &created.UpdatedAt,
	)
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint") {
			return nil, fmt.Errorf("%w: %v", ErrTrunkConflict, err)
		}
		if strings.Contains(msg, "check constraint") {
			return nil, fmt.Errorf("%w: %v", ErrTrunkValidation, err)
		}
		return nil, fmt.Errorf("insert trunk failed: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create failed: %w", err)
	}

	// Update in-memory cache.
	tm.mu.Lock()
	if created.Enabled {
		tm.trunks[created.ID] = created
		if created.PublicID != "" {
			tm.trunkByPublic[created.PublicID] = created.ID
		}
	}
	if created.IsDefault {
		for id, trunk := range tm.trunks {
			if id != created.ID {
				trunk.IsDefault = false
			}
		}
	}
	tm.mu.Unlock()

	fmt.Printf("✅ [TrunkManager] Created trunk %d (%s) %s@%s:%d/%s\n",
		created.ID, created.Name, created.Username, created.Domain, created.Port, created.Transport)

	return created, nil
}

// UpdateTrunk applies partial updates to a trunk and returns the latest state.
func (tm *TrunkManager) UpdateTrunk(ctx context.Context, trunkID int64, patch TrunkUpdatePatch) (*Trunk, error) {
	if tm.db == nil {
		return nil, fmt.Errorf("database not available for trunk manager")
	}

	// Normalize patch values.
	if patch.Name != nil {
		v := strings.TrimSpace(*patch.Name)
		if v == "" {
			return nil, fmt.Errorf("%w: name cannot be empty", ErrTrunkValidation)
		}
		patch.Name = &v
	}
	if patch.Domain != nil {
		v := strings.TrimSpace(*patch.Domain)
		if v == "" {
			return nil, fmt.Errorf("%w: domain cannot be empty", ErrTrunkValidation)
		}
		patch.Domain = &v
	}
	if patch.Username != nil {
		v := strings.TrimSpace(*patch.Username)
		if v == "" {
			return nil, fmt.Errorf("%w: username cannot be empty", ErrTrunkValidation)
		}
		patch.Username = &v
	}
	if patch.Port != nil && (*patch.Port < 1 || *patch.Port > 65535) {
		return nil, fmt.Errorf("%w: port must be between 1 and 65535", ErrTrunkValidation)
	}
	if patch.Transport != nil {
		v := strings.ToLower(strings.TrimSpace(*patch.Transport))
		if v != "tcp" && v != "udp" {
			return nil, fmt.Errorf("%w: transport must be tcp or udp", ErrTrunkValidation)
		}
		patch.Transport = &v
	}
	if patch.Password != nil {
		v := strings.TrimSpace(*patch.Password)
		if v == "" {
			patch.Password = nil
		} else {
			patch.Password = &v
		}
	}

	tx, err := tm.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin transaction failed: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	current := &Trunk{}
	err = tx.QueryRow(ctx, `
		SELECT id, public_id, name, domain, port, username, password, transport, enabled, is_default,
		       lease_owner, lease_until, last_registered_at, last_error, created_at, updated_at
		FROM sip_trunks
		WHERE id = $1
		FOR UPDATE
	`, trunkID).Scan(
		&current.ID, &current.PublicID, &current.Name, &current.Domain, &current.Port,
		&current.Username, &current.Password, &current.Transport,
		&current.Enabled, &current.IsDefault,
		&current.LeaseOwner, &current.LeaseUntil,
		&current.LastRegisteredAt, &current.LastError,
		&current.CreatedAt, &current.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: trunk %d", ErrTrunkNotFound, trunkID)
		}
		return nil, fmt.Errorf("load trunk failed: %w", err)
	}

	if patch.IsDefault != nil && *patch.IsDefault {
		if _, err := tx.Exec(ctx, `
			UPDATE sip_trunks
			SET is_default = false, updated_at = NOW()
			WHERE id <> $1 AND is_default = true
		`, trunkID); err != nil {
			return nil, fmt.Errorf("clear default trunk failed: %w", err)
		}
	}

	setClauses := make([]string, 0, 9)
	args := []interface{}{}
	argIdx := 1

	addSet := func(column string, value interface{}) {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", column, argIdx))
		args = append(args, value)
		argIdx++
	}

	if patch.Name != nil {
		addSet("name", *patch.Name)
	}
	if patch.Domain != nil {
		addSet("domain", *patch.Domain)
	}
	if patch.Port != nil {
		addSet("port", *patch.Port)
	}
	if patch.Username != nil {
		addSet("username", *patch.Username)
	}
	if patch.Password != nil {
		addSet("password", *patch.Password)
	}
	if patch.Transport != nil {
		addSet("transport", *patch.Transport)
	}
	if patch.Enabled != nil {
		addSet("enabled", *patch.Enabled)
	}
	if patch.IsDefault != nil {
		addSet("is_default", *patch.IsDefault)
	}
	if patch.UpdatedBy != nil {
		// Reserved for future audit log integration.
		_ = *patch.UpdatedBy
	}

	if len(setClauses) > 0 {
		setClauses = append(setClauses, "updated_at = NOW()")
		updateSQL := fmt.Sprintf(
			"UPDATE sip_trunks SET %s WHERE id = $%d",
			strings.Join(setClauses, ", "),
			argIdx,
		)
		args = append(args, trunkID)
		if _, err := tx.Exec(ctx, updateSQL, args...); err != nil {
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint") {
				return nil, fmt.Errorf("%w: %v", ErrTrunkConflict, err)
			}
			if strings.Contains(msg, "check constraint") {
				return nil, fmt.Errorf("%w: %v", ErrTrunkValidation, err)
			}
			return nil, fmt.Errorf("update trunk failed: %w", err)
		}
	}

	updated := &Trunk{}
	err = tx.QueryRow(ctx, `
		SELECT id, public_id, name, domain, port, username, password, transport, enabled, is_default,
		       lease_owner, lease_until, last_registered_at, last_error, created_at, updated_at
		FROM sip_trunks
		WHERE id = $1
	`, trunkID).Scan(
		&updated.ID, &updated.PublicID, &updated.Name, &updated.Domain, &updated.Port,
		&updated.Username, &updated.Password, &updated.Transport,
		&updated.Enabled, &updated.IsDefault,
		&updated.LeaseOwner, &updated.LeaseUntil,
		&updated.LastRegisteredAt, &updated.LastError,
		&updated.CreatedAt, &updated.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("reload trunk failed: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit update failed: %w", err)
	}

	tm.mu.Lock()
	if patch.IsDefault != nil && *patch.IsDefault {
		for id, trunk := range tm.trunks {
			if id != trunkID {
				trunk.IsDefault = false
			}
		}
	}
	if updated.Enabled {
		tm.trunks[trunkID] = updated
		if updated.PublicID != "" {
			tm.trunkByPublic[updated.PublicID] = trunkID
		}
	} else {
		delete(tm.trunks, trunkID)
		if updated.PublicID != "" {
			delete(tm.trunkByPublic, updated.PublicID)
		}
	}
	tm.mu.Unlock()

	if patch.Enabled != nil && !*patch.Enabled {
		if err := tm.UnregisterTrunk(trunkID, true); err != nil {
			fmt.Printf("⚠️ [TrunkManager] update trunk %d disabled, unregister failed: %v\n", trunkID, err)
		}
		fresh, freshErr := tm.GetTrunkByIDFromDB(ctx, trunkID)
		if freshErr == nil {
			return fresh, nil
		}
	}

	return updated, nil
}

// UnregisterTrunk unregisters a trunk and releases lease
func (tm *TrunkManager) RegisterTrunk(trunkID int64, force bool) error {
	if tm.db == nil {
		return fmt.Errorf("database not available for trunk manager")
	}

	ctx, cancel := tm.dbContext()
	defer cancel()

	trunk, err := tm.getTrunkByIDFromDB(ctx, trunkID)
	if err != nil {
		return err
	}
	if !trunk.Enabled {
		return fmt.Errorf("trunk %d is disabled", trunkID)
	}

	tm.mu.Lock()
	tm.trunks[trunkID] = trunk
	if trunk.PublicID != "" {
		tm.trunkByPublic[trunk.PublicID] = trunkID
	}
	tm.mu.Unlock()

	if force {
		if err := tm.acquireLeaseForce(trunkID); err != nil {
			return err
		}
	} else {
		if err := tm.acquireLease(trunkID); err != nil {
			return err
		}
	}

	if err := tm.registerTrunk(trunkID); err != nil {
		return err
	}

	return nil
}

// UnregisterTrunk unregisters a trunk and releases lease
func (tm *TrunkManager) UnregisterTrunk(trunkID int64, force bool) error {
	if tm.db == nil {
		return fmt.Errorf("database not available for trunk manager")
	}

	ctx, cancel := tm.dbContext()
	defer cancel()

	trunk, err := tm.getTrunkByIDFromDB(ctx, trunkID)
	if err != nil {
		return err
	}

	// Stop refresh worker and clear registration cache
	tm.mu.Lock()
	if stopCh, exists := tm.refreshWorkers[trunkID]; exists {
		close(stopCh)
		delete(tm.refreshWorkers, trunkID)
	}
	delete(tm.registrations, trunkID)
	delete(tm.ownedLeases, trunkID)
	if loaded, ok := tm.trunks[trunkID]; ok {
		loaded.LeaseOwner = nil
		loaded.LeaseUntil = nil
	}
	tm.mu.Unlock()

	unregisterErr := tm.sendUnregister(trunk)
	if force {
		tm.releaseLeaseForce(trunkID)
	} else {
		tm.releaseLease(trunkID)
	}

	if unregisterErr != nil {
		tm.updateRegistrationError(trunkID, unregisterErr.Error())
		return unregisterErr
	}
	tm.updateUnregisteredStatus(trunkID)

	return nil
}

// DeleteTrunk removes a trunk from the database (hard delete)
func (tm *TrunkManager) DeleteTrunk(trunkID int64, force bool) error {
	if tm.db == nil {
		return fmt.Errorf("database not available for trunk manager")
	}
	if !force && !tm.IsOwnedTrunk(trunkID) {
		return fmt.Errorf("trunk %d lease not owned by this instance", trunkID)
	}

	ctx, cancel := tm.dbContext()
	defer cancel()

	trunk, err := tm.getTrunkByIDFromDB(ctx, trunkID)
	if err != nil {
		return err
	}

	// Stop refresh worker and clear caches
	tm.mu.Lock()
	if stopCh, exists := tm.refreshWorkers[trunkID]; exists {
		close(stopCh)
		delete(tm.refreshWorkers, trunkID)
	}
	delete(tm.registrations, trunkID)
	delete(tm.ownedLeases, trunkID)
	if trunk.PublicID != "" {
		delete(tm.trunkByPublic, trunk.PublicID)
	}
	delete(tm.trunks, trunkID)
	tm.mu.Unlock()

	unregisterErr := tm.sendUnregister(trunk)

	result, err := tm.db.Exec(ctx, `DELETE FROM sip_trunks WHERE id = $1`, trunkID)
	if err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("trunk %d not found", trunkID)
	}

	if unregisterErr != nil {
		return fmt.Errorf("trunk deleted but unregister failed: %w", unregisterErr)
	}
	return nil
}

func (tm *TrunkManager) releaseLeaseForce(trunkID int64) {
	ctx, cancel := tm.dbContext()
	defer cancel()

	_, err := tm.db.Exec(ctx, `
		UPDATE sip_trunks
		SET lease_owner = NULL, lease_until = NULL, updated_at = NOW()
		WHERE id = $1
	`, trunkID)
	if err != nil {
		fmt.Printf("⚠️ [TrunkManager] Failed to release lease for trunk %d: %v\n", trunkID, err)
	} else {
		fmt.Printf("📞 [TrunkManager] Released lease for trunk %d (force)\n", trunkID)
	}

	tm.mu.Lock()
	delete(tm.ownedLeases, trunkID)
	if trunk, ok := tm.trunks[trunkID]; ok {
		trunk.LeaseOwner = nil
		trunk.LeaseUntil = nil
	}
	tm.mu.Unlock()
}

func (tm *TrunkManager) updateUnregisteredStatus(trunkID int64) {
	ctx, cancel := tm.dbContext()
	defer cancel()

	_, err := tm.db.Exec(ctx, `
		UPDATE sip_trunks
		SET last_registered_at = NULL, last_error = NULL, updated_at = NOW()
		WHERE id = $1
	`, trunkID)
	if err != nil {
		fmt.Printf("⚠️ [TrunkManager] Failed to clear registration status for trunk %d: %v\n", trunkID, err)
	}

	tm.mu.Lock()
	if trunk, ok := tm.trunks[trunkID]; ok {
		trunk.LastRegisteredAt = nil
		trunk.LastError = nil
	}
	tm.mu.Unlock()
}

func (tm *TrunkManager) acquireLeaseForce(trunkID int64) error {
	leaseTTL := time.Duration(tm.cfg.SIPTrunk.LeaseTTLSeconds) * time.Second
	leaseUntil := time.Now().Add(leaseTTL)

	ctx, cancel := tm.dbContext()
	defer cancel()

	result, err := tm.db.Exec(ctx, `
		UPDATE sip_trunks
		SET lease_owner = $1, lease_until = $2, updated_at = NOW()
		WHERE id = $3
		  AND enabled = true
	`, tm.instanceID, leaseUntil, trunkID)
	if err != nil {
		return fmt.Errorf("force lease update failed: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("trunk %d not found or disabled", trunkID)
	}

	tm.mu.Lock()
	tm.ownedLeases[trunkID] = true
	if trunk, ok := tm.trunks[trunkID]; ok {
		lo := tm.instanceID
		trunk.LeaseOwner = &lo
		trunk.LeaseUntil = &leaseUntil
	}
	tm.mu.Unlock()

	fmt.Printf("📞 [TrunkManager] Force-acquired lease for trunk %d (until %s)\n", trunkID, leaseUntil.Format(time.RFC3339))
	return nil
}

// GetTrunkByIDFromDB returns a single trunk by ID from the database.
func (tm *TrunkManager) GetTrunkByIDFromDB(ctx context.Context, trunkID int64) (*Trunk, error) {
	return tm.getTrunkByIDFromDB(ctx, trunkID)
}

func (tm *TrunkManager) getTrunkByIDFromDB(ctx context.Context, trunkID int64) (*Trunk, error) {
	if tm.db == nil {
		return nil, fmt.Errorf("database not available for trunk manager")
	}

	trunk := &Trunk{}
	err := tm.db.QueryRow(ctx, `
		SELECT id, public_id, name, domain, port, username, password, transport, enabled, is_default,
		       lease_owner, lease_until, last_registered_at, last_error, created_at, updated_at
		FROM sip_trunks
		WHERE id = $1
	`, trunkID).Scan(
		&trunk.ID, &trunk.PublicID, &trunk.Name, &trunk.Domain, &trunk.Port,
		&trunk.Username, &trunk.Password, &trunk.Transport,
		&trunk.Enabled, &trunk.IsDefault,
		&trunk.LeaseOwner, &trunk.LeaseUntil,
		&trunk.LastRegisteredAt, &trunk.LastError,
		&trunk.CreatedAt, &trunk.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("trunk %d not found", trunkID)
		}
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return trunk, nil
}

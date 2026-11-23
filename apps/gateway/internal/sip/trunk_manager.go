package sip

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"webrtc-sip-gateway/internal/config"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	SortBy        string     // Sort field: id, name, domain, port, username, enabled, created_at, updated_at (default: id)
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
	ErrTrunkLeaseLost  = errors.New("trunk lease lost")
	ErrTrunkLeaseRetry = errors.New("trunk lease renew transient failure")
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

// TrunkPushContact contains Contact header push parameters for a trunk REGISTER.
type TrunkPushContact struct {
	PNAppID string
	PNType  string
	PNToken string
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

// MobileTrunkPayload defines SIP credentials provisioned from a mobile access token.
type MobileTrunkPayload struct {
	Subject  string
	Domain   string
	Username string
	Password string
}

// AgentTrunkPayload defines SIP credentials supplied by a PC agent WebSocket client.
type AgentTrunkPayload struct {
	Domain   string
	Username string
	Password string
	Port     int // 0 defaults to 5060
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
	LastRegisteredAt   *time.Time
	LastUnregisteredAt *time.Time
	LastError          *string
	SipAutoRegister    bool

	// Active call tracking (set to JWT subject on call start, cleared on hangup)
	InUseBy *string

	// Push notification target: Keycloak sub (UUID), persisted across sessions
	NotifyUserID *string

	// Latest authenticated mobile platform that resolved this trunk.
	LastOnlinePlatform *string
	LastOnlineAt       *time.Time

	// SIP Contact push parameters for Kamailio PN integration.
	PNAppID     *string
	PNType      *string
	PNToken     *string
	PNUpdatedAt *time.Time

	// FCM registration token for /ws-agent-device sticky incoming push.
	FcmToken     *string
	FcmUpdatedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

type trunkIdentityKey struct {
	username string
	domain   string
	port     int
}

const (
	asyncDBWriteConcurrency = 32
	asyncDBWriteTimeout     = 3 * time.Second
)

// TrunkManager manages SIP trunk registrations with DB-based lease
type TrunkManager struct {
	db         trunkDB
	cfg        *config.Config
	userAgent  *sipgo.UserAgent
	sipClient  *sipgo.Client
	instanceID string
	publicIP   string
	localPort  int

	mu                sync.RWMutex
	singleInstance    bool
	stopping          bool
	asyncDBSem        chan struct{}
	inUseByGen        map[int64]uint64             // Monotonic per-trunk generation for in_use_by mutations
	inUseByFlushedGen map[int64]uint64             // Highest inUseByGen successfully persisted to DB
	inUseByWriteMu    map[int64]*sync.Mutex        // Per-trunk mutex serializing in_use_by DB writes
	trunks            map[int64]*Trunk             // All loaded trunks (CoW immutable once stored)
	trunkByPublic     map[string]int64             // normalized public_id -> trunk ID
	trunksByUsername  map[string][]int64           // lowercased trimmed username -> sorted trunk IDs
	trunksByIdentity  map[trunkIdentityKey][]int64 // lowercased trimmed (username, domain, port) -> sorted trunk IDs
	ownedLeases       map[int64]bool               // Trunks we currently own
	refreshWorkers    map[int64]chan struct{}      // Stop signals for refresh workers
	leaseRetryRuns    map[int64]int                // Consecutive lease-renew transient failures
	// registrationAnnounced is log state only: true after this process has
	// completed a healthy REGISTER. Cleared on error, lease loss, or unregister
	// so the next success is a visible recovery. It does not affect SIP.
	registrationAnnounced map[int64]bool
	// needsReRegister marks trunks whose credentials, enabled state, or push
	// contact changed since the last successful SIP REGISTER so
	// EnsureTrunkRegistered cannot skip re-registration.
	needsReRegister map[int64]bool
	// trunkConfigGen is a monotonic per-trunk generation incremented whenever
	// registration-relevant credentials, endpoint, transport, enabled state, or
	// push contact change. registerTrunk snapshots this before sending REGISTER
	// so updateRegistrationSuccessWithContact will not clear needsReRegister if
	// a newer config update landed while REGISTER was in flight.
	trunkConfigGen map[int64]uint64
	// registeredPushContacts records the push contact parameters included in
	// the last successful SIP REGISTER for each trunk.
	registeredPushContacts map[int64]TrunkPushContact

	registrarIdentities  map[int64]*registrarIdentity
	registrarGenerations map[int64]uint64
	resolver             registrarEndpointResolver
	// registerRetries holds backoff for owned trunks whose last REGISTER
	// failed; the lease heartbeat retries them.
	registerRetries map[int64]*registerRetry

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	onTrunkListChange TrunkListChangeFunc
}

type trunkDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

// TrunkInviteMatchResult contains detailed invite matching outcome for observability.
type TrunkInviteMatchResult struct {
	Trunk             *Trunk
	Owned             bool
	Rule              string
	CandidateIDs      []int64
	SIPUser           string
	OwnedCandidates   []int64
	Ambiguous         bool
	LocalRequestURI   bool
	LocalToURI        bool
	Origins           []string
	EligibleIDs       []int64
	RegistrarEvidence string
	Reason            string
}

const trunkManagerDBTimeout = 5 * time.Second

const sipTrunkSelectColumns = `id, public_id, name, domain, port, username, password, transport, enabled, is_default,
		       lease_owner, lease_until, last_registered_at, last_unregistered_at, last_error, sip_auto_register, in_use_by, notify_user_id,
		       last_online_platform, last_online_at,
		       pn_app_id, pn_type, pn_token, pn_updated_at, created_at, updated_at, fcm_token, fcm_updated_at`

const sipTrunkUpsertNoOpSafeSQL = `
	WITH upserted AS (
		INSERT INTO sip_trunks (
			public_id, name, domain, port, username, password, transport, enabled, is_default
		)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, false)
		ON CONFLICT (name) DO UPDATE
		SET domain = EXCLUDED.domain,
		    port = EXCLUDED.port,
		    username = EXCLUDED.username,
		    password = EXCLUDED.password,
		    transport = EXCLUDED.transport,
		    enabled = true,
		    updated_at = NOW()
		WHERE (sip_trunks.domain, sip_trunks.port, sip_trunks.username, sip_trunks.password, sip_trunks.transport, sip_trunks.enabled)
		   IS DISTINCT FROM (EXCLUDED.domain, EXCLUDED.port, EXCLUDED.username, EXCLUDED.password, EXCLUDED.transport, true)
		RETURNING ` + sipTrunkSelectColumns + `
	)
	SELECT ` + sipTrunkSelectColumns + `
	FROM upserted
	UNION ALL
	SELECT ` + sipTrunkSelectColumns + `
	FROM sip_trunks
	WHERE name = $1
	  AND NOT EXISTS (SELECT 1 FROM upserted)
	LIMIT 1
`

type trunkRowScanner interface {
	Scan(dest ...any) error
}

func scanTrunkRow(row trunkRowScanner, trunk *Trunk) error {
	return row.Scan(
		&trunk.ID, &trunk.PublicID, &trunk.Name, &trunk.Domain, &trunk.Port,
		&trunk.Username, &trunk.Password, &trunk.Transport,
		&trunk.Enabled, &trunk.IsDefault,
		&trunk.LeaseOwner, &trunk.LeaseUntil,
		&trunk.LastRegisteredAt, &trunk.LastUnregisteredAt, &trunk.LastError, &trunk.SipAutoRegister, &trunk.InUseBy, &trunk.NotifyUserID,
		&trunk.LastOnlinePlatform, &trunk.LastOnlineAt,
		&trunk.PNAppID, &trunk.PNType, &trunk.PNToken, &trunk.PNUpdatedAt,
		&trunk.CreatedAt, &trunk.UpdatedAt,
		&trunk.FcmToken, &trunk.FcmUpdatedAt,
	)
}

func dbContextWithTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, trunkManagerDBTimeout)
}

func trunkPushContactFromTrunk(trunk *Trunk) TrunkPushContact {
	if trunk == nil {
		return TrunkPushContact{}
	}
	return TrunkPushContact{
		PNAppID: trimStringPtr(trunk.PNAppID),
		PNType:  trimStringPtr(trunk.PNType),
		PNToken: trimStringPtr(trunk.PNToken),
	}
}

func trunkRegistrationConfigChanged(previous, updated *Trunk) bool {
	if previous == nil || updated == nil {
		return false
	}
	return trunkRegistrarIdentityChanged(previous, updated) ||
		previous.Password != updated.Password ||
		previous.Enabled != updated.Enabled ||
		trunkPushContactFromTrunk(previous) != trunkPushContactFromTrunk(updated)
}

// TrunkListChangeFunc is invoked when trunk list rows change in ways visible to admin UI.
type TrunkListChangeFunc func(eventType string, trunkIDs []int64)

func (tm *TrunkManager) SetTrunkListChangeCallback(fn TrunkListChangeFunc) {
	tm.onTrunkListChange = fn
}

func (tm *TrunkManager) emitTrunkListChange(eventType string, trunkIDs ...int64) {
	if tm.onTrunkListChange == nil || len(trunkIDs) == 0 {
		return
	}
	tm.onTrunkListChange(eventType, trunkIDs)
}

func (tm *TrunkManager) dbContext() (context.Context, context.CancelFunc) {
	base := tm.ctx
	if base == nil {
		base = context.Background()
	}
	return context.WithTimeout(base, trunkManagerDBTimeout)
}

func (tm *TrunkManager) leaseTTLDuration() time.Duration {
	if tm != nil && tm.cfg != nil && tm.cfg.SIPTrunk.LeaseTTLSeconds > 0 {
		return time.Duration(tm.cfg.SIPTrunk.LeaseTTLSeconds) * time.Second
	}
	return 60 * time.Second
}

// NewTrunkManager creates a new trunk manager
func NewTrunkManager(db *pgxpool.Pool, cfg *config.Config, userAgent *sipgo.UserAgent, instanceID string) *TrunkManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &TrunkManager{
		db:                     db,
		cfg:                    cfg,
		userAgent:              userAgent,
		instanceID:             instanceID,
		publicIP:               cfg.SIP.PublicIP,
		localPort:              cfg.SIP.LocalPort,
		asyncDBSem:             make(chan struct{}, asyncDBWriteConcurrency),
		inUseByGen:             make(map[int64]uint64),
		inUseByFlushedGen:      make(map[int64]uint64),
		inUseByWriteMu:         make(map[int64]*sync.Mutex),
		trunks:                 make(map[int64]*Trunk),
		trunkByPublic:          make(map[string]int64),
		trunksByUsername:       make(map[string][]int64),
		trunksByIdentity:       make(map[trunkIdentityKey][]int64),
		ownedLeases:            make(map[int64]bool),
		refreshWorkers:         make(map[int64]chan struct{}),
		leaseRetryRuns:         make(map[int64]int),
		registrationAnnounced:  make(map[int64]bool),
		needsReRegister:        make(map[int64]bool),
		trunkConfigGen:         make(map[int64]uint64),
		registeredPushContacts: make(map[int64]TrunkPushContact),
		registrarIdentities:    make(map[int64]*registrarIdentity),
		registrarGenerations:   make(map[int64]uint64),
		resolver:               defaultRegistrarResolver{},
		ctx:                    ctx,
		cancel:                 cancel,
	}
}

// SetSingleInstance enables or disables single-instance mode.
// When enabled, TrunkManager eliminates DB lease churn and uses 0-sync-DB RAM
// fast paths for outbound call claim, trunk heartbeat, and registration checks.
func (tm *TrunkManager) SetSingleInstance(enabled bool) {
	if tm == nil {
		return
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.singleInstance = enabled
	if enabled {
		tm.markLoadedEnabledTrunksOwnedLocked()
	}
}

// IsSingleInstance reports whether single-instance mode is enabled.
func (tm *TrunkManager) IsSingleInstance() bool {
	if tm == nil {
		return false
	}
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.singleInstance
}

func cloneStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneTimePtr(p *time.Time) *time.Time {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneTrunk(trunk *Trunk) *Trunk {
	if trunk == nil {
		return nil
	}
	copied := *trunk
	copied.LeaseOwner = cloneStringPtr(trunk.LeaseOwner)
	copied.LeaseUntil = cloneTimePtr(trunk.LeaseUntil)
	copied.LastRegisteredAt = cloneTimePtr(trunk.LastRegisteredAt)
	copied.LastUnregisteredAt = cloneTimePtr(trunk.LastUnregisteredAt)
	copied.LastError = cloneStringPtr(trunk.LastError)
	copied.InUseBy = cloneStringPtr(trunk.InUseBy)
	copied.NotifyUserID = cloneStringPtr(trunk.NotifyUserID)
	copied.LastOnlinePlatform = cloneStringPtr(trunk.LastOnlinePlatform)
	copied.LastOnlineAt = cloneTimePtr(trunk.LastOnlineAt)
	copied.PNAppID = cloneStringPtr(trunk.PNAppID)
	copied.PNType = cloneStringPtr(trunk.PNType)
	copied.PNToken = cloneStringPtr(trunk.PNToken)
	copied.PNUpdatedAt = cloneTimePtr(trunk.PNUpdatedAt)
	copied.FcmToken = cloneStringPtr(trunk.FcmToken)
	copied.FcmUpdatedAt = cloneTimePtr(trunk.FcmUpdatedAt)
	return &copied
}

func trunkPublicIDIndexKey(publicID string) string {
	if normalized, ok := NormalizeTrunkPublicID(publicID); ok {
		return normalized
	}
	return strings.TrimSpace(publicID)
}

func makeTrunkUsernameKey(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func makeTrunkIdentityKey(username, domain string, port int) trunkIdentityKey {
	return trunkIdentityKey{
		username: makeTrunkUsernameKey(username),
		domain:   normalizeSIPHost(domain),
		port:     normalizeSIPPort(port),
	}
}

func insertSortedInt64(ids []int64, target int64) []int64 {
	i := sort.Search(len(ids), func(idx int) bool { return ids[idx] >= target })
	if i < len(ids) && ids[i] == target {
		return ids
	}
	ids = append(ids, 0)
	copy(ids[i+1:], ids[i:])
	ids[i] = target
	return ids
}

func removeSortedInt64(ids []int64, target int64) []int64 {
	i := sort.Search(len(ids), func(idx int) bool { return ids[idx] >= target })
	if i >= len(ids) || ids[i] != target {
		return ids
	}
	copy(ids[i:], ids[i+1:])
	return ids[:len(ids)-1]
}

// rebuildTrunkIndexesLocked rebuilds all secondary indexes from tm.trunks.
// Caller must hold tm.mu.Lock().
func (tm *TrunkManager) rebuildTrunkIndexesLocked() {
	if tm.trunks == nil {
		tm.trunks = make(map[int64]*Trunk)
	}
	tm.trunkByPublic = make(map[string]int64, len(tm.trunks))
	tm.trunksByUsername = make(map[string][]int64, len(tm.trunks))
	tm.trunksByIdentity = make(map[trunkIdentityKey][]int64, len(tm.trunks))
	for id, trunk := range tm.trunks {
		if trunk == nil {
			continue
		}
		if pubKey := trunkPublicIDIndexKey(trunk.PublicID); pubKey != "" {
			tm.trunkByPublic[pubKey] = id
		}
		if userKey := makeTrunkUsernameKey(trunk.Username); userKey != "" {
			tm.trunksByUsername[userKey] = insertSortedInt64(tm.trunksByUsername[userKey], id)
		}
		identKey := makeTrunkIdentityKey(trunk.Username, trunk.Domain, trunk.Port)
		if identKey.username != "" && identKey.domain != "" {
			tm.trunksByIdentity[identKey] = insertSortedInt64(tm.trunksByIdentity[identKey], id)
		}
	}
}

// ensureTrunkIndexesLocked initializes secondary indexes if they have not been
// built yet (e.g. when a test constructs &TrunkManager{trunks: ...} directly).
// Caller must hold tm.mu.Lock().
func (tm *TrunkManager) ensureTrunkIndexesLocked() {
	if tm.trunks == nil {
		tm.trunks = make(map[int64]*Trunk)
	}
	if tm.trunksByUsername != nil && tm.trunksByIdentity != nil {
		if tm.trunkByPublic == nil {
			tm.trunkByPublic = make(map[string]int64, len(tm.trunks))
			for id, trunk := range tm.trunks {
				if trunk != nil {
					if pubKey := trunkPublicIDIndexKey(trunk.PublicID); pubKey != "" {
						tm.trunkByPublic[pubKey] = id
					}
				}
			}
		}
		return
	}
	tm.rebuildTrunkIndexesLocked()
}

// setTrunkLocked stores a CoW-safe *Trunk in tm.trunks and updates all secondary indexes.
// Caller must hold tm.mu.Lock().
func (tm *TrunkManager) setTrunkLocked(trunk *Trunk) {
	if trunk == nil {
		return
	}
	tm.ensureTrunkIndexesLocked()

	previous := tm.trunks[trunk.ID]
	if previous != nil {
		oldPub := trunkPublicIDIndexKey(previous.PublicID)
		newPub := trunkPublicIDIndexKey(trunk.PublicID)
		if oldPub != "" && oldPub != newPub && tm.trunkByPublic[oldPub] == trunk.ID {
			delete(tm.trunkByPublic, oldPub)
		}
		oldUser := makeTrunkUsernameKey(previous.Username)
		newUser := makeTrunkUsernameKey(trunk.Username)
		if oldUser != "" && oldUser != newUser {
			if updated := removeSortedInt64(tm.trunksByUsername[oldUser], trunk.ID); len(updated) == 0 {
				delete(tm.trunksByUsername, oldUser)
			} else {
				tm.trunksByUsername[oldUser] = updated
			}
		}
		oldIdent := makeTrunkIdentityKey(previous.Username, previous.Domain, previous.Port)
		newIdent := makeTrunkIdentityKey(trunk.Username, trunk.Domain, trunk.Port)
		if oldIdent.username != "" && oldIdent.domain != "" && oldIdent != newIdent {
			if updated := removeSortedInt64(tm.trunksByIdentity[oldIdent], trunk.ID); len(updated) == 0 {
				delete(tm.trunksByIdentity, oldIdent)
			} else {
				tm.trunksByIdentity[oldIdent] = updated
			}
		}
	}

	tm.trunks[trunk.ID] = trunk
	if pubKey := trunkPublicIDIndexKey(trunk.PublicID); pubKey != "" {
		tm.trunkByPublic[pubKey] = trunk.ID
	}
	if userKey := makeTrunkUsernameKey(trunk.Username); userKey != "" {
		tm.trunksByUsername[userKey] = insertSortedInt64(tm.trunksByUsername[userKey], trunk.ID)
	}
	identKey := makeTrunkIdentityKey(trunk.Username, trunk.Domain, trunk.Port)
	if identKey.username != "" && identKey.domain != "" {
		tm.trunksByIdentity[identKey] = insertSortedInt64(tm.trunksByIdentity[identKey], trunk.ID)
	}
}

// removeTrunkLocked deletes trunkID from tm.trunks and all secondary indexes.
// Caller must hold tm.mu.Lock().
func (tm *TrunkManager) removeTrunkLocked(trunkID int64, extraPublicID string) {
	previous, existed := tm.trunks[trunkID]
	delete(tm.trunks, trunkID)

	if tm.trunkByPublic != nil {
		if existed && previous != nil {
			if oldPub := trunkPublicIDIndexKey(previous.PublicID); oldPub != "" && tm.trunkByPublic[oldPub] == trunkID {
				delete(tm.trunkByPublic, oldPub)
			}
		}
		if extraPub := trunkPublicIDIndexKey(extraPublicID); extraPub != "" && tm.trunkByPublic[extraPub] == trunkID {
			delete(tm.trunkByPublic, extraPub)
		}
	}
	if !existed || previous == nil {
		return
	}
	if tm.trunksByUsername != nil {
		if oldUser := makeTrunkUsernameKey(previous.Username); oldUser != "" {
			if updated := removeSortedInt64(tm.trunksByUsername[oldUser], trunkID); len(updated) == 0 {
				delete(tm.trunksByUsername, oldUser)
			} else {
				tm.trunksByUsername[oldUser] = updated
			}
		}
	}
	if tm.trunksByIdentity != nil {
		oldIdent := makeTrunkIdentityKey(previous.Username, previous.Domain, previous.Port)
		if oldIdent.username != "" && oldIdent.domain != "" {
			if updated := removeSortedInt64(tm.trunksByIdentity[oldIdent], trunkID); len(updated) == 0 {
				delete(tm.trunksByIdentity, oldIdent)
			} else {
				tm.trunksByIdentity[oldIdent] = updated
			}
		}
	}
}

// updateTrunkCoWLocked clones the cached *Trunk for trunkID, applies mutate to
// the clone, and atomically swaps it into tm.trunks (updating secondary indexes
// if any indexed field changed). Never mutates an already-stored *Trunk in place.
// Caller must hold tm.mu.Lock().
func (tm *TrunkManager) updateTrunkCoWLocked(trunkID int64, mutate func(*Trunk)) *Trunk {
	current, ok := tm.trunks[trunkID]
	if !ok || current == nil {
		return nil
	}
	updated := cloneTrunk(current)
	mutate(updated)
	if updated.PublicID != current.PublicID ||
		updated.Username != current.Username ||
		updated.Domain != current.Domain ||
		updated.Port != current.Port {
		tm.setTrunkLocked(updated)
	} else {
		tm.trunks[trunkID] = updated
	}
	return updated
}

// markLoadedEnabledTrunksOwnedLocked marks every loaded enabled trunk as owned
// by this instance in single-instance mode without DB lease writes.
// Caller must hold tm.mu.Lock().
func (tm *TrunkManager) markLoadedEnabledTrunksOwnedLocked() {
	if tm.ownedLeases == nil {
		tm.ownedLeases = make(map[int64]bool, len(tm.trunks))
	}
	for id := range tm.ownedLeases {
		if trunk, ok := tm.trunks[id]; !ok || trunk == nil || !trunk.Enabled {
			delete(tm.ownedLeases, id)
		}
	}
	for id, trunk := range tm.trunks {
		if trunk == nil || !trunk.Enabled {
			continue
		}
		tm.ownedLeases[id] = true
		if trunk.LeaseOwner == nil || *trunk.LeaseOwner != tm.instanceID {
			tm.updateTrunkCoWLocked(id, func(t *Trunk) {
				lo := tm.instanceID
				t.LeaseOwner = &lo
			})
		}
	}
}

func (tm *TrunkManager) getAsyncDBSem() chan struct{} {
	tm.mu.RLock()
	sem := tm.asyncDBSem
	tm.mu.RUnlock()
	if sem != nil {
		return sem
	}
	tm.mu.Lock()
	if tm.asyncDBSem == nil {
		tm.asyncDBSem = make(chan struct{}, asyncDBWriteConcurrency)
	}
	sem = tm.asyncDBSem
	tm.mu.Unlock()
	return sem
}

func (tm *TrunkManager) withLocalLeaseOwner(trunk *Trunk) *Trunk {
	if trunk == nil {
		return nil
	}
	if trunk.LeaseOwner != nil && *trunk.LeaseOwner == tm.instanceID {
		return trunk
	}
	copied := cloneTrunk(trunk)
	lo := tm.instanceID
	copied.LeaseOwner = &lo
	return copied
}

func (tm *TrunkManager) bumpInUseByGenLocked(trunkID int64) uint64 {
	if tm.inUseByGen == nil {
		tm.inUseByGen = make(map[int64]uint64)
	}
	tm.inUseByGen[trunkID]++
	return tm.inUseByGen[trunkID]
}

func (tm *TrunkManager) getInUseByWriteMu(trunkID int64) *sync.Mutex {
	tm.mu.RLock()
	if tm.inUseByWriteMu != nil {
		if mu := tm.inUseByWriteMu[trunkID]; mu != nil {
			tm.mu.RUnlock()
			return mu
		}
	}
	tm.mu.RUnlock()

	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.inUseByWriteMu == nil {
		tm.inUseByWriteMu = make(map[int64]*sync.Mutex)
	}
	mu := tm.inUseByWriteMu[trunkID]
	if mu == nil {
		mu = &sync.Mutex{}
		tm.inUseByWriteMu[trunkID] = mu
	}
	return mu
}

func (tm *TrunkManager) enqueueAsyncInUseByUpdate(trunkID int64, claimUsername *string, expectedGen uint64) {
	if tm == nil || tm.db == nil {
		return
	}
	sem := tm.getAsyncDBSem()
	select {
	case sem <- struct{}{}:
	default:
		fmt.Printf("⚠️ [TrunkManager] Async in_use_by DB write dropped for trunk %d: concurrency limit reached\n", trunkID)
		return
	}

	tm.mu.Lock()
	if tm.stopping || (tm.inUseByFlushedGen != nil && tm.inUseByFlushedGen[trunkID] >= expectedGen) {
		tm.mu.Unlock()
		<-sem
		return
	}
	tm.wg.Add(1)
	tm.mu.Unlock()

	fallbackVal := cloneStringPtr(claimUsername)
	go func() {
		defer tm.wg.Done()
		defer func() { <-sem }()

		writeMu := tm.getInUseByWriteMu(trunkID)
		writeMu.Lock()
		defer writeMu.Unlock()

		tm.mu.RLock()
		currentGen := uint64(0)
		if tm.inUseByGen != nil {
			currentGen = tm.inUseByGen[trunkID]
		}
		flushedGen := uint64(0)
		if tm.inUseByFlushedGen != nil {
			flushedGen = tm.inUseByFlushedGen[trunkID]
		}
		if currentGen != 0 && flushedGen >= currentGen {
			tm.mu.RUnlock()
			return
		}
		valToWrite := fallbackVal
		targetGen := expectedGen
		if currentGen > targetGen {
			targetGen = currentGen
		}
		if trunk, ok := tm.trunks[trunkID]; ok && trunk != nil {
			valToWrite = cloneStringPtr(trunk.InUseBy)
		}
		tm.mu.RUnlock()

		ctx, cancel := context.WithTimeout(context.Background(), asyncDBWriteTimeout)
		defer cancel()
		if _, err := tm.db.Exec(ctx, `
			UPDATE sip_trunks SET in_use_by = $1, updated_at = NOW() WHERE id = $2
		`, valToWrite, trunkID); err != nil {
			fmt.Printf("⚠️ [TrunkManager] Async in_use_by update failed for trunk %d: %v\n", trunkID, err)
			return
		}

		tm.mu.Lock()
		if tm.inUseByFlushedGen == nil {
			tm.inUseByFlushedGen = make(map[int64]uint64)
		}
		if targetGen > tm.inUseByFlushedGen[trunkID] {
			tm.inUseByFlushedGen[trunkID] = targetGen
		}
		tm.mu.Unlock()
	}()
}

// BuildMobileTrunkName returns the deterministic DB trunk name for a mobile auth subject.
func BuildMobileTrunkName(subject string) (string, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "", fmt.Errorf("%w: subject is required", ErrTrunkValidation)
	}
	return "sipclient-mobile-" + subject, nil
}

// UpsertMobileTrunk creates or updates the deterministic SIP trunk for a mobile auth subject.
func (tm *TrunkManager) UpsertMobileTrunk(ctx context.Context, payload MobileTrunkPayload) (*Trunk, error) {
	if tm.db == nil {
		return nil, fmt.Errorf("database not available for trunk manager")
	}

	name, err := BuildMobileTrunkName(payload.Subject)
	if err != nil {
		return nil, err
	}
	domain := strings.TrimSpace(payload.Domain)
	username := strings.TrimSpace(payload.Username)
	password := strings.TrimSpace(payload.Password)
	if domain == "" {
		return nil, fmt.Errorf("%w: domain is required", ErrTrunkValidation)
	}
	if username == "" {
		return nil, fmt.Errorf("%w: username is required", ErrTrunkValidation)
	}
	if password == "" {
		return nil, fmt.Errorf("%w: password is required", ErrTrunkValidation)
	}

	const (
		port      = 5060
		transport = "tcp"
	)

	trunk, err := tm.upsertTrunkByName(ctx, name, domain, port, username, password, transport, "upsert mobile trunk failed")
	if err != nil {
		return nil, err
	}

	tm.cacheUpsertedTrunk(trunk)

	return trunk, nil
}

// BuildAgentTrunkName returns the deterministic DB trunk name for a PC agent SIP identity.
// The name always uses the sipclient-agent- prefix and cannot collide with mobile trunks
// (sipclient-mobile-<subject>).
func BuildAgentTrunkName(domain, username string, port int) (string, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	username = strings.TrimSpace(username)
	if domain == "" {
		return "", fmt.Errorf("%w: domain is required", ErrTrunkValidation)
	}
	if username == "" {
		return "", fmt.Errorf("%w: username is required", ErrTrunkValidation)
	}
	if port <= 0 {
		port = 5060
	}
	return fmt.Sprintf("sipclient-agent-%s@%s:%d", username, domain, port), nil
}

const agentDeviceTrunkNamePrefix = "sipclient-agent-device-"
const agentTrunkNamePrefix = "sipclient-agent-"

// IsAgentDeviceTrunkName reports whether name uses the agent-device namespace.
func IsAgentDeviceTrunkName(name string) bool {
	return strings.HasPrefix(strings.TrimSpace(name), agentDeviceTrunkNamePrefix)
}

// IsAgentTrunkName reports whether name uses the PC agent namespace (not agent-device).
func IsAgentTrunkName(name string) bool {
	name = strings.TrimSpace(name)
	return strings.HasPrefix(name, agentTrunkNamePrefix) && !IsAgentDeviceTrunkName(name)
}

// BuildAgentDeviceTrunkName returns the deterministic DB trunk name for an agent-device SIP identity.
func BuildAgentDeviceTrunkName(domain, username string, port int) (string, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	username = strings.TrimSpace(username)
	if domain == "" {
		return "", fmt.Errorf("%w: domain is required", ErrTrunkValidation)
	}
	if username == "" {
		return "", fmt.Errorf("%w: username is required", ErrTrunkValidation)
	}
	if port <= 0 {
		port = 5060
	}
	return fmt.Sprintf("sipclient-agent-device-%s@%s:%d", username, domain, port), nil
}

// UpsertAgentTrunk creates or updates the deterministic SIP trunk for a PC agent identity.
func (tm *TrunkManager) UpsertAgentTrunk(ctx context.Context, payload AgentTrunkPayload) (*Trunk, error) {
	if tm.db == nil {
		return nil, fmt.Errorf("database not available for trunk manager")
	}

	port := payload.Port
	if port <= 0 {
		port = 5060
	}
	name, err := BuildAgentTrunkName(payload.Domain, payload.Username, port)
	if err != nil {
		return nil, err
	}
	domain := strings.ToLower(strings.TrimSpace(payload.Domain))
	username := strings.TrimSpace(payload.Username)
	password := strings.TrimSpace(payload.Password)
	if password == "" {
		return nil, fmt.Errorf("%w: password is required", ErrTrunkValidation)
	}

	const transport = "tcp"

	trunk, err := tm.upsertTrunkByName(ctx, name, domain, port, username, password, transport, "upsert agent trunk failed")
	if err != nil {
		return nil, err
	}

	tm.cacheUpsertedTrunk(trunk)

	return trunk, nil
}

// UpsertAgentDeviceTrunk creates or updates the sticky SIP trunk for an agent-device identity.
func (tm *TrunkManager) UpsertAgentDeviceTrunk(ctx context.Context, payload AgentTrunkPayload) (*Trunk, error) {
	if tm.db == nil {
		return nil, fmt.Errorf("database not available for trunk manager")
	}

	port := payload.Port
	if port <= 0 {
		port = 5060
	}
	name, err := BuildAgentDeviceTrunkName(payload.Domain, payload.Username, port)
	if err != nil {
		return nil, err
	}
	domain := strings.ToLower(strings.TrimSpace(payload.Domain))
	username := strings.TrimSpace(payload.Username)
	password := strings.TrimSpace(payload.Password)
	if password == "" {
		return nil, fmt.Errorf("%w: password is required", ErrTrunkValidation)
	}

	const transport = "tcp"

	trunk, err := tm.upsertTrunkByName(ctx, name, domain, port, username, password, transport, "upsert agent-device trunk failed")
	if err != nil {
		return nil, err
	}

	tm.cacheUpsertedTrunk(trunk)

	return trunk, nil
}

func (tm *TrunkManager) upsertTrunkByName(ctx context.Context, name, domain string, port int, username, password, transport, errPrefix string) (*Trunk, error) {
	dbCtx, cancel := dbContextWithTimeout(ctx)
	defer cancel()

	trunk := &Trunk{}
	err := scanTrunkRow(
		tm.db.QueryRow(dbCtx, sipTrunkUpsertNoOpSafeSQL, name, domain, port, username, password, transport, true),
		trunk,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Rare concurrent first-insert race where ON CONFLICT saw a newly committed
		// identical row invisible to the CTE's statement snapshot; re-read with fresh snapshot.
		err = scanTrunkRow(
			tm.db.QueryRow(dbCtx, `SELECT `+sipTrunkSelectColumns+` FROM sip_trunks WHERE name = $1 LIMIT 1`, name),
			trunk,
		)
	}
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "check constraint") {
			return nil, fmt.Errorf("%w: %v", ErrTrunkValidation, err)
		}
		return nil, fmt.Errorf("%s: %w", errPrefix, err)
	}
	return trunk, nil
}

func (tm *TrunkManager) cacheUpsertedTrunk(trunk *Trunk) {
	if trunk == nil {
		return
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	previous := tm.trunks[trunk.ID]
	configChanged := trunkRegistrationConfigChanged(previous, trunk)
	if trunk.Enabled {
		toCache := trunk
		if tm.singleInstance {
			toCache = tm.withLocalLeaseOwner(trunk)
			if tm.ownedLeases == nil {
				tm.ownedLeases = make(map[int64]bool)
			}
			tm.ownedLeases[trunk.ID] = true
		}
		tm.setTrunkLocked(toCache)
		if configChanged {
			if tm.needsReRegister == nil {
				tm.needsReRegister = make(map[int64]bool)
			}
			tm.needsReRegister[trunk.ID] = true
			tm.bumpTrunkConfigGenLocked(trunk.ID)
		}
	} else {
		tm.removeTrunkLocked(trunk.ID, trunk.PublicID)
		delete(tm.ownedLeases, trunk.ID)
		delete(tm.needsReRegister, trunk.ID)
		delete(tm.registeredPushContacts, trunk.ID)
		tm.bumpTrunkConfigGenLocked(trunk.ID)
	}

	if trunkRegistrarIdentityChanged(previous, trunk) || !trunk.Enabled {
		tm.invalidateRegistrarIdentityLocked(trunk.ID)
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

	// Start lease heartbeat / registration retry worker.
	// In single-instance mode, DB lease renewal is skipped inside the worker
	// while failed REGISTER retry backoff remains active.
	tm.mu.Lock()
	if !tm.stopping {
		tm.wg.Add(1)
		go tm.leaseHeartbeatWorker()
	}
	tm.mu.Unlock()

	fmt.Printf("✅ [TrunkManager] Started with instance_id=%s (single_instance=%v)\n", tm.instanceID, tm.IsSingleInstance())
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
		// Admin refresh reloads trunk state from the DB; bad credentials or
		// offline registrars must not fail the whole operation (same as startup).
		fmt.Printf("⚠️ [TrunkManager] Refresh reloaded trunks but failed to register some: %v\n", err)
	}
	return nil
}

// trunkHasActiveRegistrationLocked reports whether trunkID has an active SIP
// registration that should be unregistered on shutdown.
// Caller must hold tm.mu (read or write).
func (tm *TrunkManager) trunkHasActiveRegistrationLocked(trunkID int64) bool {
	if _, hasWorker := tm.refreshWorkers[trunkID]; hasWorker {
		return true
	}
	if _, hasIdentity := tm.registrarIdentities[trunkID]; hasIdentity {
		return true
	}
	if trunk := tm.trunks[trunkID]; trunk != nil && trunk.SipAutoRegister && trunk.LastRegisteredAt != nil {
		return true
	}
	return false
}

// Stop releases all leases and stops workers
func (tm *TrunkManager) Stop() {
	if tm == nil {
		return
	}
	if tm.cfg != nil && !tm.cfg.SIPTrunk.Enable {
		return
	}

	fmt.Printf("📞 [TrunkManager] Stopping...\n")

	// Mark stopping before cancelling/waiting so no concurrent caller can call
	// tm.wg.Add(1) while or after tm.wg.Wait() executes.
	tm.mu.Lock()
	tm.stopping = true
	ownedIDs := make([]int64, 0, len(tm.ownedLeases))
	unregisterIDs := make([]int64, 0, len(tm.ownedLeases))
	for id := range tm.ownedLeases {
		ownedIDs = append(ownedIDs, id)
		if tm.trunkHasActiveRegistrationLocked(id) {
			unregisterIDs = append(unregisterIDs, id)
		}
	}
	tm.mu.Unlock()

	// Cancel context to stop workers
	if tm.cancel != nil {
		tm.cancel()
	}

	// Unregister only trunks with an active SIP registration (best-effort)
	sort.Slice(unregisterIDs, func(i, j int) bool { return unregisterIDs[i] < unregisterIDs[j] })
	for _, id := range unregisterIDs {
		tm.unregisterTrunk(id)
	}
	tm.releaseAllLeases(ownedIDs)

	// Wait for workers to finish
	tm.wg.Wait()

	tm.mu.Lock()
	tm.registrarIdentities = make(map[int64]*registrarIdentity)
	tm.registrarGenerations = make(map[int64]uint64)
	tm.needsReRegister = make(map[int64]bool)
	tm.trunkConfigGen = make(map[int64]uint64)
	tm.registeredPushContacts = make(map[int64]TrunkPushContact)
	tm.mu.Unlock()

	fmt.Printf("✅ [TrunkManager] Stopped\n")
}

// releaseAllLeases releases all leases owned by this instance in a single SQL statement.
func (tm *TrunkManager) releaseAllLeases(ownedIDs []int64) {
	if len(ownedIDs) == 0 {
		return
	}

	if tm.db != nil && !tm.IsSingleInstance() {
		ctx, cancel := context.WithTimeout(context.Background(), trunkManagerDBTimeout)
		defer cancel()

		_, err := tm.db.Exec(ctx, `
			UPDATE sip_trunks
			SET lease_owner = NULL, lease_until = NULL, updated_at = NOW()
			WHERE lease_owner = $1
		`, tm.instanceID)
		if err != nil {
			fmt.Printf("⚠️ [TrunkManager] Failed to release leases for instance %s: %v\n", tm.instanceID, err)
		} else {
			fmt.Printf("📞 [TrunkManager] Released %d lease(s) for instance %s\n", len(ownedIDs), tm.instanceID)
		}
	}

	tm.mu.Lock()
	for _, id := range ownedIDs {
		delete(tm.ownedLeases, id)
		delete(tm.leaseRetryRuns, id)
		delete(tm.needsReRegister, id)
		delete(tm.registeredPushContacts, id)
		tm.updateTrunkCoWLocked(id, func(t *Trunk) {
			t.LeaseOwner = nil
			t.LeaseUntil = nil
		})
	}
	tm.mu.Unlock()

	tm.emitTrunkListChange("lease_updated", ownedIDs...)
}

// loadTrunks loads enabled trunks from database
func (tm *TrunkManager) loadTrunks() error {
	ctx, cancel := tm.dbContext()
	defer cancel()

	rows, err := tm.db.Query(ctx, `
		SELECT id, public_id, name, domain, port, username, password, transport, enabled, is_default,
		       lease_owner, lease_until, last_registered_at, last_unregistered_at, last_error, sip_auto_register, in_use_by, notify_user_id,
		       last_online_platform, last_online_at,
		       pn_app_id, pn_type, pn_token, pn_updated_at, created_at, updated_at, fcm_token, fcm_updated_at
		FROM sip_trunks
		WHERE enabled = true
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	loaded := make(map[int64]*Trunk)
	trunkByPublic := make(map[string]int64)
	count := 0
	for rows.Next() {
		trunk := &Trunk{}
		err := rows.Scan(
			&trunk.ID, &trunk.PublicID, &trunk.Name, &trunk.Domain, &trunk.Port,
			&trunk.Username, &trunk.Password, &trunk.Transport,
			&trunk.Enabled, &trunk.IsDefault,
			&trunk.LeaseOwner, &trunk.LeaseUntil,
			&trunk.LastRegisteredAt, &trunk.LastUnregisteredAt, &trunk.LastError, &trunk.SipAutoRegister, &trunk.InUseBy, &trunk.NotifyUserID,
			&trunk.LastOnlinePlatform, &trunk.LastOnlineAt,
			&trunk.PNAppID, &trunk.PNType, &trunk.PNToken, &trunk.PNUpdatedAt,
			&trunk.CreatedAt, &trunk.UpdatedAt,
			&trunk.FcmToken, &trunk.FcmUpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}
		if trunk.PublicID != "" {
			trunkByPublic[trunk.PublicID] = trunk.ID
		}

		loaded[trunk.ID] = trunk
		count++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows iteration failed: %w", err)
	}

	tm.replaceLoadedTrunks(loaded, trunkByPublic)

	fmt.Printf("📞 [TrunkManager] Loaded %d enabled trunk(s) from DB\n", count)
	return nil
}

func (tm *TrunkManager) replaceLoadedTrunks(loaded map[int64]*Trunk, trunkByPublic map[string]int64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	invalidated := make(map[int64]struct{})
	invalidate := func(trunkID int64) {
		if _, exists := invalidated[trunkID]; exists {
			return
		}
		tm.invalidateRegistrarIdentityLocked(trunkID)
		invalidated[trunkID] = struct{}{}
	}

	for id, previous := range tm.trunks {
		updated, exists := loaded[id]
		if !exists || trunkRegistrarIdentityChanged(previous, updated) || !updated.Enabled {
			invalidate(id)
		}
		if !exists || trunkRegistrationConfigChanged(previous, updated) || !updated.Enabled {
			tm.bumpTrunkConfigGenLocked(id)
		}
	}
	for id := range tm.registrarIdentities {
		previous, previouslyLoaded := tm.trunks[id]
		updated, stillLoaded := loaded[id]
		if !previouslyLoaded || !stillLoaded || trunkRegistrarIdentityChanged(previous, updated) || !updated.Enabled {
			invalidate(id)
		}
	}

	tm.trunks = loaded
	tm.rebuildTrunkIndexesLocked()
	for pubKey, id := range trunkByPublic {
		if normalized := trunkPublicIDIndexKey(pubKey); normalized != "" {
			if _, exists := tm.trunkByPublic[normalized]; !exists {
				tm.trunkByPublic[normalized] = id
			}
		}
	}
	if tm.singleInstance {
		tm.markLoadedEnabledTrunksOwnedLocked()
	}
}

// acquireAndRegisterAll tries to acquire leases and register all loaded trunks
func (tm *TrunkManager) acquireAndRegisterAll() error {
	if tm.IsSingleInstance() {
		tm.mu.Lock()
		tm.markLoadedEnabledTrunksOwnedLocked()
		trunkIDs := make([]int64, 0, len(tm.trunks))
		for id, trunk := range tm.trunks {
			if !trunkWantsAutoRegister(trunk) {
				fmt.Printf("📞 [TrunkManager] Skipping restore registration for trunk %d (restore disabled)\n", id)
				continue
			}
			trunkIDs = append(trunkIDs, id)
		}
		tm.mu.Unlock()

		sort.Slice(trunkIDs, func(i, j int) bool { return trunkIDs[i] < trunkIDs[j] })

		var firstErr error
		for _, id := range trunkIDs {
			if err := tm.registerTrunk(id); err != nil {
				fmt.Printf("⚠️ [TrunkManager] Failed to register trunk %d: %v\n", id, err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		return firstErr
	}

	tm.mu.RLock()
	trunkIDs := make([]int64, 0, len(tm.trunks))
	for id, trunk := range tm.trunks {
		if !trunkWantsAutoRegister(trunk) {
			fmt.Printf("📞 [TrunkManager] Skipping restore registration for trunk %d (restore disabled)\n", id)
			continue
		}
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

func trunkWantsAutoRegister(t *Trunk) bool {
	return t != nil && t.Enabled && t.SipAutoRegister
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
		return fmt.Errorf("%w: lease update failed for trunk %d on instance %s: %v", ErrTrunkLeaseRetry, trunkID, tm.instanceID, err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("%w: trunk %d lease held by another instance or trunk disabled", ErrTrunkLeaseLost, trunkID)
	}

	// Successfully acquired/renewed lease
	tm.mu.Lock()
	wasOwned := tm.ownedLeases[trunkID]
	if tm.ownedLeases == nil {
		tm.ownedLeases = make(map[int64]bool)
	}
	tm.ownedLeases[trunkID] = true
	delete(tm.leaseRetryRuns, trunkID)
	tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
		lo := tm.instanceID
		lu := leaseUntil
		t.LeaseOwner = &lo
		t.LeaseUntil = &lu
	})
	tm.mu.Unlock()

	if !wasOwned {
		tm.emitTrunkListChange("lease_updated", trunkID)
	}

	// Renewing a lease this instance already owns is a routine heartbeat.
	tm.logTrunkInfoOrDebug(!wasOwned, "📞 [TrunkManager] Acquired lease for trunk %d (until %s)\n", trunkID, leaseUntil.Format(time.RFC3339))
	return nil
}

// setRegistrationAnnouncedLocked updates log-only REGISTER announcement state.
// Caller must hold tm.mu.
func (tm *TrunkManager) setRegistrationAnnouncedLocked(trunkID int64, announced bool) {
	if tm.registrationAnnounced == nil {
		if !announced {
			return
		}
		tm.registrationAnnounced = make(map[int64]bool)
	}
	if announced {
		tm.registrationAnnounced[trunkID] = true
		return
	}
	delete(tm.registrationAnnounced, trunkID)
}

func (tm *TrunkManager) sipTrunkDebug() bool {
	return tm != nil && tm.cfg != nil && tm.cfg.SIP.DebugSIPTrunk
}

func (tm *TrunkManager) logTrunkDebug(format string, args ...any) {
	if !tm.sipTrunkDebug() {
		return
	}
	fmt.Printf(format, args...)
}

// logTrunkInfoOrDebug prints at INFO when info is set. Routine cycles pass
// info=false and are emitted only when DEBUG_SIP_TRUNK is on.
func (tm *TrunkManager) logTrunkInfoOrDebug(info bool, format string, args ...any) {
	if info {
		fmt.Printf(format, args...)
		return
	}
	tm.logTrunkDebug(format, args...)
}

func (tm *TrunkManager) markLeaseLostLocked(trunkID int64) {
	if stopCh, exists := tm.refreshWorkers[trunkID]; exists {
		close(stopCh)
		delete(tm.refreshWorkers, trunkID)
	}
	tm.invalidateRegistrarIdentityLocked(trunkID)
	delete(tm.ownedLeases, trunkID)
	delete(tm.registerRetries, trunkID)
	delete(tm.leaseRetryRuns, trunkID)
	delete(tm.needsReRegister, trunkID)
	delete(tm.registeredPushContacts, trunkID)
	tm.setRegistrationAnnouncedLocked(trunkID, false)
	tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
		t.LeaseOwner = nil
		t.LeaseUntil = nil
	})
}

func (tm *TrunkManager) markLeaseLost(trunkID int64) {
	tm.mu.Lock()
	tm.markLeaseLostLocked(trunkID)
	tm.mu.Unlock()

	tm.emitTrunkListChange("lease_updated", trunkID)
}

func (tm *TrunkManager) incrementLeaseRetry(trunkID int64) int {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.leaseRetryRuns[trunkID]++
	return tm.leaseRetryRuns[trunkID]
}

func (tm *TrunkManager) resetLeaseRetry(trunkID int64) {
	tm.mu.Lock()
	delete(tm.leaseRetryRuns, trunkID)
	tm.mu.Unlock()
}

func (tm *TrunkManager) handleLeaseRenewResult(trunkID int64, err error) (leaseLost bool, retryCount int) {
	if err == nil {
		tm.resetLeaseRetry(trunkID)
		return false, 0
	}

	if errors.Is(err, ErrTrunkLeaseLost) {
		tm.markLeaseLost(trunkID)
		return true, 0
	}

	return false, tm.incrementLeaseRetry(trunkID)
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
	delete(tm.leaseRetryRuns, trunkID)
	tm.mu.Unlock()

	tm.emitTrunkListChange("lease_updated", trunkID)
}

// registerTrunk performs SIP REGISTER for a trunk. A failure on a trunk this
// instance owns is retried with backoff from the lease heartbeat.
func (tm *TrunkManager) registerTrunk(trunkID int64) (err error) {
	tm.mu.Lock()
	trunk, ok := tm.trunks[trunkID]
	var generation uint64
	var configGen uint64
	previouslyAnnounced := false
	if ok {
		trunk = cloneTrunk(trunk)
		generation = tm.nextRegistrarGenerationLocked(trunkID)
		configGen = tm.trunkConfigGen[trunkID]
		previouslyAnnounced = tm.registrationAnnounced[trunkID]
	}
	tm.mu.Unlock()

	if !ok {
		return fmt.Errorf("trunk %d not found", trunkID)
	}
	defer func() {
		if err != nil {
			tm.noteRegisterFailure(trunkID)
		}
	}()

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
	req.AppendHeader(tm.trunkContactHeader(trunk))

	// Expires
	expires := tm.cfg.SIPPublic.RegisterExpiresSeconds
	req.AppendHeader(sip.NewHeader("Expires", fmt.Sprintf("%d", expires)))

	// User-Agent
	req.AppendHeader(sip.NewHeader("User-Agent", "WebRTC-SIP-Gateway-Trunk/1.0"))

	// Resolve dialable destination (host:port) for SetDestination()
	destination, err := resolveSIPDestination(trunk.Domain, trunk.Port, trunk.Transport)
	if err != nil {
		tm.mu.Lock()
		tm.setRegistrationAnnouncedLocked(trunkID, false)
		tm.mu.Unlock()
		return fmt.Errorf("failed to resolve destination: %w", err)
	}
	tm.logTrunkInfoOrDebug(!previouslyAnnounced, "📞 [TrunkManager] Trunk %d REGISTER target=%s transport=%s contact=sip:%s@%s:%d\n",
		trunkID,
		destination,
		trunk.Transport,
		trunk.Username,
		tm.publicIP,
		tm.localPort,
	)
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
		tm.logTrunkDebug("📞 [TrunkManager] Trunk %d: Received %d, retrying with auth\n", trunkID, res.StatusCode)

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

	// UnregisterTrunk or a lost lease may have run while this REGISTER was in
	// flight (its Expires:0 can even reach the registrar first). Do not report
	// success or restart the refresh worker for a binding nobody wants.
	owned, current := tm.registerOutcomeStillWanted(trunkID, generation)
	if !owned {
		fmt.Printf("📞 [TrunkManager] Trunk %d REGISTER completed after unregister or lease loss; releasing binding\n", trunkID)
		if releaseErr := tm.releaseTrunkContact(trunk); releaseErr != nil {
			fmt.Printf("⚠️ [TrunkManager] Trunk %d stale binding release failed: %v\n", trunkID, releaseErr)
		}
		return fmt.Errorf("trunk %d is no longer owned by this instance", trunkID)
	}
	if !current {
		// A newer REGISTER for this trunk is in flight and owns the outcome.
		tm.logTrunkDebug("📞 [TrunkManager] Trunk %d superseded REGISTER result ignored (generation=%d)\n", trunkID, generation)
		return nil
	}

	tm.publishRegistrarIdentityFromRegister(trunk, generation, res, expires)

	// Success
	tm.updateRegistrationSuccessWithContact(trunkID, trunkPushContactFromTrunk(trunk), configGen)

	// Start refresh worker if not already running
	tm.startRefreshWorker(trunkID, expires)

	tm.logTrunkInfoOrDebug(!previouslyAnnounced, "✅ [TrunkManager] Trunk %d registered successfully (expires in %ds)\n", trunkID, expires)
	return nil
}

// unregisterTrunk sends REGISTER with Expires:0 (best-effort)
func (tm *TrunkManager) unregisterTrunk(trunkID int64) {
	tm.mu.Lock()
	trunk, ok := tm.trunks[trunkID]
	if !ok {
		tm.mu.Unlock()
		return
	}
	tm.invalidateRegistrarIdentityLocked(trunkID)
	// Close and delete under one lock: markLeaseLost and startRefreshWorker
	// close the same channel, and closing it twice panics.
	if stopCh, hasWorker := tm.refreshWorkers[trunkID]; hasWorker {
		close(stopCh)
		delete(tm.refreshWorkers, trunkID)
	}
	tm.mu.Unlock()

	if err := tm.releaseTrunkContact(trunk); err != nil {
		fmt.Printf("⚠️ [TrunkManager] Trunk %d unregister failed (ignored): %v\n", trunkID, err)
	}

	tm.mu.Lock()
	delete(tm.leaseRetryRuns, trunkID)
	tm.setRegistrationAnnouncedLocked(trunkID, false)
	tm.mu.Unlock()
}

// releaseTrunkContact removes this trunk's SIP binding. Agent and agent-device
// trunks for the same username/domain/port share one Contact URI, so removing it
// would also drop the sibling that is still online. In that case the sibling is
// refreshed and the shared binding stays.
func (tm *TrunkManager) releaseTrunkContact(trunk *Trunk) error {
	if trunk == nil {
		return nil
	}
	siblings := tm.liveSharedAgentContactSiblingIDs(trunk)
	if len(siblings) == 0 {
		if err := tm.sendUnregister(trunk); err != nil {
			return err
		}
		fmt.Printf("📞 [TrunkManager] Trunk %d unregistered\n", trunk.ID)
		return nil
	}

	fmt.Printf("📞 [TrunkManager] Trunk %d kept shared SIP contact; refreshing sibling trunks %v\n", trunk.ID, siblings)
	if tm.sipClient == nil {
		fmt.Printf("⚠️ [TrunkManager] Trunk %d sibling contact refresh skipped: SIP client not initialized\n", trunk.ID)
		return nil
	}
	for _, id := range siblings {
		if err := tm.registerTrunk(id); err != nil {
			fmt.Printf("⚠️ [TrunkManager] Trunk %d sibling %d REGISTER refresh failed: %v\n", trunk.ID, id, err)
		}
	}
	return nil
}

func (tm *TrunkManager) liveSharedAgentContactSiblingIDs(trunk *Trunk) []int64 {
	if tm == nil || trunk == nil {
		return nil
	}
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.liveSharedAgentContactSiblingIDsLocked(trunk)
}

func (tm *TrunkManager) liveSharedAgentContactSiblingIDsLocked(trunk *Trunk) []int64 {
	if trunk == nil || (!IsAgentTrunkName(trunk.Name) && !IsAgentDeviceTrunkName(trunk.Name)) {
		return nil
	}
	user := strings.TrimSpace(trunk.Username)
	domain := normalizeSIPHost(trunk.Domain)
	port := normalizeSIPPort(trunk.Port)
	now := time.Now()
	ids := make([]int64, 0, 1)
	if tm.trunksByIdentity != nil {
		key := makeTrunkIdentityKey(trunk.Username, trunk.Domain, trunk.Port)
		for _, id := range tm.trunksByIdentity[key] {
			if id == trunk.ID || !tm.ownedLeases[id] {
				continue
			}
			other := tm.trunks[id]
			if other == nil {
				continue
			}
			if strings.TrimSpace(other.Username) != user || normalizeSIPHost(other.Domain) != domain || normalizeSIPPort(other.Port) != port {
				continue
			}
			if !IsAgentTrunkName(other.Name) && !IsAgentDeviceTrunkName(other.Name) {
				continue
			}
			_, refreshing := tm.refreshWorkers[id]
			if !refreshing && !tm.registrarIdentities[id].isCurrent(now) {
				continue
			}
			ids = append(ids, id)
		}
		return ids
	}
	for id, other := range tm.trunks {
		if other == nil || id == trunk.ID || !tm.ownedLeases[id] {
			continue
		}
		if strings.TrimSpace(other.Username) != user || normalizeSIPHost(other.Domain) != domain || normalizeSIPPort(other.Port) != port {
			continue
		}
		if !IsAgentTrunkName(other.Name) && !IsAgentDeviceTrunkName(other.Name) {
			continue
		}
		_, refreshing := tm.refreshWorkers[id]
		if !refreshing && !tm.registrarIdentities[id].isCurrent(now) {
			continue
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
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
	req.AppendHeader(tm.trunkContactHeader(trunk))

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

func (tm *TrunkManager) trunkContactHeader(trunk *Trunk) *sip.ContactHeader {
	contact := &sip.ContactHeader{
		Address: sip.Uri{
			User: trunk.Username,
			Host: tm.publicIP,
			Port: tm.localPort,
		},
	}

	if trunk.PNAppID != nil && trunk.PNType != nil && trunk.PNToken != nil {
		appID := strings.TrimSpace(*trunk.PNAppID)
		pnType := strings.TrimSpace(*trunk.PNType)
		pnToken := strings.TrimSpace(*trunk.PNToken)
		if appID != "" && pnType != "" && pnToken != "" {
			params := sip.NewParams()
			params.Add("app-id", appID)
			params.Add("pn-type", pnType)
			params.Add("pn-tok", pnToken)
			contact.Params = params
		}
	}

	return contact
}

// startRefreshWorker starts a goroutine to refresh registration before expiry
func (tm *TrunkManager) startRefreshWorker(trunkID int64, expiresSeconds int) {
	tm.mu.Lock()
	if tm.stopping {
		tm.mu.Unlock()
		return
	}
	// Stop existing worker if any
	if stopCh, exists := tm.refreshWorkers[trunkID]; exists {
		close(stopCh)
	}

	stopCh := make(chan struct{})
	tm.refreshWorkers[trunkID] = stopCh
	tm.wg.Add(1)
	tm.mu.Unlock()

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
				tm.logTrunkDebug("📞 [TrunkManager] Trunk %d: Refresh worker stopped\n", trunkID)
				return

			case <-tm.ctx.Done():
				return
			}
		}
	}()
}

// leaseHeartbeatWorker periodically renews leases for owned trunks and retries failed registrations.
// In single-instance mode, DB lease renewal is skipped while registration retry backoff remains active.
func (tm *TrunkManager) leaseHeartbeatWorker() {
	defer tm.wg.Done()

	renewInterval := time.Duration(tm.cfg.SIPTrunk.LeaseRenewInterval) * time.Second
	if renewInterval <= 0 {
		renewInterval = 20 * time.Second
	}
	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !tm.IsSingleInstance() {
				tm.mu.RLock()
				ownedIDs := make([]int64, 0, len(tm.ownedLeases))
				for id := range tm.ownedLeases {
					ownedIDs = append(ownedIDs, id)
				}
				tm.mu.RUnlock()

				tm.renewOwnedLeasesBatch(ownedIDs)
			}
			tm.retryFailedRegistrations(time.Now())

		case <-tm.ctx.Done():
			fmt.Printf("📞 [TrunkManager] Lease heartbeat worker stopped\n")
			return
		}
	}
}

// renewOwnedLeasesBatch renews all owned trunk leases in a single SQL statement
// and reconciles renewed vs lost leases under one tm.mu.Lock() pass.
func (tm *TrunkManager) renewOwnedLeasesBatch(ownedIDs []int64) {
	if len(ownedIDs) == 0 || tm.db == nil {
		return
	}

	leaseUntil := time.Now().Add(tm.leaseTTLDuration())
	ctx, cancel := tm.dbContext()
	defer cancel()

	rows, err := tm.db.Query(ctx, `
		UPDATE sip_trunks
		SET lease_owner = $3, lease_until = $1, updated_at = NOW()
		WHERE id = ANY($2)
		  AND enabled = true
		  AND (lease_until IS NULL OR lease_until < NOW() OR lease_owner = $3)
		RETURNING id
	`, leaseUntil, ownedIDs, tm.instanceID)

	var renewed map[int64]bool
	if err == nil {
		defer rows.Close()
		renewed = make(map[int64]bool, len(ownedIDs))
		for rows.Next() {
			var id int64
			if scanErr := rows.Scan(&id); scanErr != nil {
				err = scanErr
				break
			}
			renewed[id] = true
		}
		if err == nil {
			err = rows.Err()
		}
	}

	if err != nil {
		// Transient DB failure: retain ownership and increment retry counters in one lock pass.
		type retryLog struct {
			id    int64
			count int
		}
		logs := make([]retryLog, 0, len(ownedIDs))
		tm.mu.Lock()
		if tm.leaseRetryRuns == nil {
			tm.leaseRetryRuns = make(map[int64]int)
		}
		for _, id := range ownedIDs {
			if !tm.ownedLeases[id] {
				continue
			}
			tm.leaseRetryRuns[id]++
			logs = append(logs, retryLog{id: id, count: tm.leaseRetryRuns[id]})
		}
		tm.mu.Unlock()

		retryErr := fmt.Errorf("%w: batched lease update failed on instance %s: %v", ErrTrunkLeaseRetry, tm.instanceID, err)
		for _, item := range logs {
			fmt.Printf("⚠️ [TrunkManager] Lease renew transient failure (retry=%d) for trunk %d on instance %s: %v\n", item.count, item.id, tm.instanceID, retryErr)
		}
		return
	}

	lostIDs := make([]int64, 0)
	tm.mu.Lock()
	for _, id := range ownedIDs {
		if !tm.ownedLeases[id] {
			continue
		}
		if renewed[id] {
			delete(tm.leaseRetryRuns, id)
			tm.updateTrunkCoWLocked(id, func(t *Trunk) {
				lo := tm.instanceID
				lu := leaseUntil
				t.LeaseOwner = &lo
				t.LeaseUntil = &lu
			})
			continue
		}
		tm.markLeaseLostLocked(id)
		lostIDs = append(lostIDs, id)
	}
	tm.mu.Unlock()

	if len(lostIDs) > 0 {
		for _, id := range lostIDs {
			fmt.Printf("⚠️ [TrunkManager] Lease lost for trunk %d on instance %s: %v\n", id, tm.instanceID, ErrTrunkLeaseLost)
		}
		tm.emitTrunkListChange("lease_updated", lostIDs...)
	}
}

func (tm *TrunkManager) bumpTrunkConfigGenLocked(trunkID int64) uint64 {
	if tm.trunkConfigGen == nil {
		tm.trunkConfigGen = make(map[int64]uint64)
	}
	tm.trunkConfigGen[trunkID]++
	return tm.trunkConfigGen[trunkID]
}

// updateRegistrationSuccess updates DB with successful registration timestamp
func (tm *TrunkManager) updateRegistrationSuccess(trunkID int64) {
	tm.mu.RLock()
	contact := trunkPushContactFromTrunk(tm.trunks[trunkID])
	configGen := tm.trunkConfigGen[trunkID]
	tm.mu.RUnlock()
	tm.updateRegistrationSuccessWithContact(trunkID, contact, configGen)
}

func (tm *TrunkManager) updateRegistrationSuccessWithContact(trunkID int64, registeredContact TrunkPushContact, registeredConfigGen uint64) {
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
	if tm.registeredPushContacts == nil {
		tm.registeredPushContacts = make(map[int64]TrunkPushContact)
	}
	tm.registeredPushContacts[trunkID] = registeredContact
	if trunk := tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
		regAt := now
		t.LastRegisteredAt = &regAt
		t.LastError = nil
	}); trunk != nil {
		if tm.trunkConfigGen[trunkID] == registeredConfigGen && trunkPushContactFromTrunk(trunk) == registeredContact {
			delete(tm.needsReRegister, trunkID)
		}
	} else {
		delete(tm.needsReRegister, trunkID)
	}
	tm.setRegistrationAnnouncedLocked(trunkID, true)
	tm.mu.Unlock()

	tm.emitTrunkListChange("registration_updated", trunkID)
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
	tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
		msg := errMsg
		t.LastError = &msg
	})
	tm.setRegistrationAnnouncedLocked(trunkID, false)
	tm.mu.Unlock()

	tm.emitTrunkListChange("registration_updated", trunkID)
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
	normalizedPublicID, ok := NormalizeTrunkPublicID(publicID)
	if !ok {
		return 0, false
	}

	tm.mu.RLock()
	trunkID, found := tm.trunkByPublic[normalizedPublicID]
	tm.mu.RUnlock()
	if found {
		return trunkID, true
	}

	if tm.db == nil {
		return 0, false
	}

	ctx, cancel := tm.dbContext()
	defer cancel()

	err := tm.db.QueryRow(ctx, `
		SELECT id
		FROM sip_trunks
		WHERE public_id = $1::uuid
		  AND enabled = true
		LIMIT 1
	`, normalizedPublicID).Scan(&trunkID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false
		}
		return 0, false
	}

	tm.mu.Lock()
	if tm.trunkByPublic == nil {
		tm.trunkByPublic = make(map[string]int64)
	}
	tm.trunkByPublic[normalizedPublicID] = trunkID
	tm.mu.Unlock()

	return trunkID, true
}

// ListOwnedTrunks returns all trunks currently owned by this instance
func (tm *TrunkManager) ListOwnedTrunks() []*Trunk {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	trunks := make([]*Trunk, 0, len(tm.ownedLeases))
	for id, owned := range tm.ownedLeases {
		if !owned {
			continue
		}
		if trunk, ok := tm.trunks[id]; ok && trunk != nil {
			trunks = append(trunks, trunk)
		}
	}
	sort.Slice(trunks, func(i, j int) bool { return trunks[i].ID < trunks[j].ID })
	return trunks
}

// GetTrunksByIDsFromRAM returns CoW-safe *Trunk pointers from RAM in requested
// order of unique positive trunk IDs, along with any missing IDs.
func (tm *TrunkManager) GetTrunksByIDsFromRAM(trunkIDs []int64) (found []*Trunk, missing []int64) {
	seen := make(map[int64]struct{}, len(trunkIDs))
	uniqueIDs := make([]int64, 0, len(trunkIDs))
	for _, id := range trunkIDs {
		if id <= 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) == 0 {
		return []*Trunk{}, nil
	}
	if tm == nil {
		return []*Trunk{}, uniqueIDs
	}

	found = make([]*Trunk, 0, len(uniqueIDs))
	tm.mu.RLock()
	for _, id := range uniqueIDs {
		if trunk, ok := tm.trunks[id]; ok && trunk != nil {
			found = append(found, trunk)
		} else {
			missing = append(missing, id)
		}
	}
	tm.mu.RUnlock()
	return found, missing
}

// ResolveTrunkByCredentialsFromRAM resolves an enabled trunk by credentials from
// the O(1) in-memory identity index. It prefers an exact port match and also
// matches port 0 against the default normalized SIP port (5060).
func (tm *TrunkManager) ResolveTrunkByCredentialsFromRAM(domain string, port int, username, password string) (*Trunk, bool) {
	if tm == nil {
		return nil, false
	}
	trimmedDomain := strings.TrimSpace(domain)
	trimmedUser := strings.TrimSpace(username)
	if trimmedDomain == "" || trimmedUser == "" || password == "" {
		return nil, false
	}
	normDomain := normalizeSIPHost(trimmedDomain)
	normPort := normalizeSIPPort(port)
	key := makeTrunkIdentityKey(trimmedUser, trimmedDomain, port)

	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var candidateIDs []int64
	if tm.trunksByIdentity != nil {
		candidateIDs = tm.trunksByIdentity[key]
	} else {
		// Fallback when tm.trunks was populated directly in unit tests without index init.
		for id, trunk := range tm.trunks {
			if trunk == nil {
				continue
			}
			if makeTrunkIdentityKey(trunk.Username, trunk.Domain, trunk.Port) == key {
				candidateIDs = append(candidateIDs, id)
			}
		}
		sort.Slice(candidateIDs, func(i, j int) bool { return candidateIDs[i] < candidateIDs[j] })
	}

	var fallbackMatch *Trunk
	for _, id := range candidateIDs {
		trunk := tm.trunks[id]
		if trunk == nil || !trunk.Enabled || trunk.Password != password {
			continue
		}
		if strings.TrimSpace(trunk.Username) != trimmedUser || normalizeSIPHost(trunk.Domain) != normDomain {
			continue
		}
		if trunk.Port == port {
			return trunk, true
		}
		if normalizeSIPPort(trunk.Port) == normPort && fallbackMatch == nil {
			fallbackMatch = trunk
		}
	}
	if fallbackMatch != nil {
		return fallbackMatch, true
	}
	return nil, false
}

// IsTrunkOwned reports whether this instance currently holds the trunk lease.
func (tm *TrunkManager) IsTrunkOwned(trunkID int64) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.ownedLeases[trunkID]
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

	dbCtx, cancel := dbContextWithTimeout(ctx)
	defer cancel()

	// Count total matching rows
	countSQL := "SELECT COUNT(*) FROM sip_trunks " + where
	var total int
	if err := tm.db.QueryRow(dbCtx, countSQL, args...).Scan(&total); err != nil {
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
		       lease_owner, lease_until, last_registered_at, last_unregistered_at, last_error, sip_auto_register, in_use_by, notify_user_id,
		       last_online_platform, last_online_at,
		       pn_app_id, pn_type, pn_token, pn_updated_at, created_at, updated_at, fcm_token, fcm_updated_at
		FROM sip_trunks
		%s
		ORDER BY %s %s
		LIMIT $%d OFFSET $%d
	`, where, sortColumn, sortDirection, argIdx, argIdx+1)
	args = append(args, params.PageSize, offset)

	rows, err := tm.db.Query(dbCtx, dataSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	trunks := make([]*Trunk, 0)
	for rows.Next() {
		trunk := &Trunk{}
		if err := scanTrunkRow(rows, trunk); err != nil {
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

	dbCtx, cancel := dbContextWithTimeout(ctx)
	defer cancel()

	tx, err := tm.db.BeginTx(dbCtx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin transaction failed: %w", err)
	}
	defer func() {
		_ = tx.Rollback(dbCtx)
	}()

	// If setting as default, clear other defaults.
	if payload.IsDefault {
		if _, err := tx.Exec(dbCtx, `
			UPDATE sip_trunks
			SET is_default = false, updated_at = NOW()
			WHERE is_default = true
		`); err != nil {
			return nil, fmt.Errorf("clear default trunk failed: %w", err)
		}
	}

	created := &Trunk{}
	publicID := uuid.NewString()
	err = tx.QueryRow(dbCtx, `
		INSERT INTO sip_trunks (public_id, name, domain, port, username, password, transport, enabled, is_default, sip_auto_register)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, false)
		RETURNING id, public_id, name, domain, port, username, password, transport, enabled, is_default,
		          lease_owner, lease_until, last_registered_at, last_unregistered_at, last_error, sip_auto_register, in_use_by, created_at, updated_at
	`, publicID, payload.Name, payload.Domain, payload.Port, payload.Username, payload.Password,
		payload.Transport, payload.Enabled, payload.IsDefault,
	).Scan(
		&created.ID, &created.PublicID, &created.Name, &created.Domain, &created.Port,
		&created.Username, &created.Password, &created.Transport,
		&created.Enabled, &created.IsDefault,
		&created.LeaseOwner, &created.LeaseUntil,
		&created.LastRegisteredAt, &created.LastUnregisteredAt, &created.LastError, &created.SipAutoRegister, &created.InUseBy,
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

	if err := tx.Commit(dbCtx); err != nil {
		return nil, fmt.Errorf("commit create failed: %w", err)
	}

	// Update in-memory cache.
	tm.mu.Lock()
	if created.Enabled {
		toCache := created
		if tm.singleInstance {
			toCache = tm.withLocalLeaseOwner(created)
			if tm.ownedLeases == nil {
				tm.ownedLeases = make(map[int64]bool)
			}
			tm.ownedLeases[created.ID] = true
		}
		tm.setTrunkLocked(toCache)
	}
	if created.IsDefault {
		for id, trunk := range tm.trunks {
			if id != created.ID && trunk != nil && trunk.IsDefault {
				tm.updateTrunkCoWLocked(id, func(t *Trunk) {
					t.IsDefault = false
				})
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

	dbCtx, cancel := dbContextWithTimeout(ctx)
	defer cancel()

	tx, err := tm.db.BeginTx(dbCtx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin transaction failed: %w", err)
	}
	defer func() {
		_ = tx.Rollback(dbCtx)
	}()

	current := &Trunk{}
	err = scanTrunkRow(tx.QueryRow(dbCtx, `
		SELECT `+sipTrunkSelectColumns+`
		FROM sip_trunks
		WHERE id = $1
		FOR UPDATE
	`, trunkID), current)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: trunk %d", ErrTrunkNotFound, trunkID)
		}
		return nil, fmt.Errorf("load trunk failed: %w", err)
	}

	if patch.IsDefault != nil && *patch.IsDefault {
		if _, err := tx.Exec(dbCtx, `
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

	updated := current
	if len(setClauses) > 0 {
		setClauses = append(setClauses, "updated_at = NOW()")
		updateSQL := fmt.Sprintf(
			"UPDATE sip_trunks SET %s WHERE id = $%d RETURNING %s",
			strings.Join(setClauses, ", "),
			argIdx,
			sipTrunkSelectColumns,
		)
		args = append(args, trunkID)
		updated = &Trunk{}
		if err := scanTrunkRow(tx.QueryRow(dbCtx, updateSQL, args...), updated); err != nil {
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

	if err := tx.Commit(dbCtx); err != nil {
		return nil, fmt.Errorf("commit update failed: %w", err)
	}

	identityChanged := trunkRegistrarIdentityChanged(current, updated)
	configChanged := trunkRegistrationConfigChanged(current, updated)
	tm.mu.Lock()
	if patch.IsDefault != nil && *patch.IsDefault {
		for id, trunk := range tm.trunks {
			if id != trunkID && trunk != nil && trunk.IsDefault {
				tm.updateTrunkCoWLocked(id, func(t *Trunk) {
					t.IsDefault = false
				})
			}
		}
	}
	if updated.Enabled {
		toCache := updated
		if tm.singleInstance {
			toCache = tm.withLocalLeaseOwner(updated)
			if tm.ownedLeases == nil {
				tm.ownedLeases = make(map[int64]bool)
			}
			tm.ownedLeases[trunkID] = true
		}
		tm.setTrunkLocked(toCache)
		if configChanged {
			if tm.needsReRegister == nil {
				tm.needsReRegister = make(map[int64]bool)
			}
			tm.needsReRegister[trunkID] = true
			tm.bumpTrunkConfigGenLocked(trunkID)
		}
	} else {
		tm.removeTrunkLocked(trunkID, updated.PublicID)
		delete(tm.ownedLeases, trunkID)
		delete(tm.needsReRegister, trunkID)
		delete(tm.registeredPushContacts, trunkID)
		tm.bumpTrunkConfigGenLocked(trunkID)
	}
	if identityChanged || !updated.Enabled {
		tm.invalidateRegistrarIdentityLocked(trunkID)
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

// EnsureTrunkRegistered is a push-wake fast-path that avoids redundant DB writes
// and SIP REGISTER round-trips when this instance already holds an unexpired lease,
// has an active refresh worker and fresh registration with no errors, and neither
// credentials nor push contact parameters have changed. Otherwise it delegates to
// RegisterTrunk(trunkID, forceLeaseSteal) to preserve multi-instance failover.
func (tm *TrunkManager) EnsureTrunkRegistered(trunkID int64, forceLeaseSteal bool) error {
	if tm.canSkipRegister(trunkID, time.Now()) {
		tm.logTrunkDebug("📞 [TrunkManager] Trunk %d already leased and registered on %s; skipping redundant REGISTER\n", trunkID, tm.instanceID)
		return nil
	}
	return tm.RegisterTrunk(trunkID, forceLeaseSteal)
}

func (tm *TrunkManager) canSkipRegister(trunkID int64, now time.Time) bool {
	if tm == nil {
		return false
	}

	leaseMargin := 5 * time.Second
	if ttl := tm.leaseTTLDuration(); ttl > 0 {
		quarterTTL := ttl / 4
		if quarterTTL > 0 && quarterTTL < leaseMargin {
			leaseMargin = quarterTTL
		}
	}

	registerExpires := 300 * time.Second
	if tm.cfg != nil && tm.cfg.SIPPublic.RegisterExpiresSeconds > 0 {
		registerExpires = time.Duration(tm.cfg.SIPPublic.RegisterExpiresSeconds) * time.Second
	}
	freshWindow := time.Duration(float64(registerExpires) * 0.9)
	if freshWindow <= 0 {
		freshWindow = registerExpires
	}

	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if !tm.singleInstance && tm.db == nil {
		return false
	}
	if !tm.ownedLeases[trunkID] {
		return false
	}
	if tm.needsReRegister[trunkID] {
		return false
	}
	if _, hasWorker := tm.refreshWorkers[trunkID]; !hasWorker {
		return false
	}
	if tm.registerRetries[trunkID] != nil {
		return false
	}

	trunk, ok := tm.trunks[trunkID]
	if !ok || trunk == nil || !trunk.Enabled || !trunk.SipAutoRegister {
		return false
	}
	if !tm.singleInstance {
		if trunk.LeaseOwner == nil || strings.TrimSpace(*trunk.LeaseOwner) != tm.instanceID {
			return false
		}
		if trunk.LeaseUntil == nil || !trunk.LeaseUntil.After(now.Add(leaseMargin)) {
			return false
		}
	}
	if trunk.LastError != nil && strings.TrimSpace(*trunk.LastError) != "" {
		return false
	}
	if trunk.LastRegisteredAt == nil {
		return false
	}
	age := now.Sub(*trunk.LastRegisteredAt)
	if age >= freshWindow {
		return false
	}

	// Verify push contact parameters match what was registered when tracked.
	if registeredContact, tracked := tm.registeredPushContacts[trunkID]; tracked {
		if registeredContact != trunkPushContactFromTrunk(trunk) {
			return false
		}
	}

	return true
}

// RegisterTrunk acquires/renews the trunk lease, enables auto-registration, and performs SIP REGISTER.
func (tm *TrunkManager) RegisterTrunk(trunkID int64, force bool) error {
	if tm.db == nil {
		return fmt.Errorf("database not available for trunk manager")
	}

	leaseUntil := time.Now().Add(tm.leaseTTLDuration())
	ctx, cancel := tm.dbContext()
	defer cancel()

	effectiveForce := force || tm.IsSingleInstance()

	var updateSQL string
	if effectiveForce {
		updateSQL = `
			UPDATE sip_trunks
			SET sip_auto_register = true,
			    lease_owner = $1,
			    lease_until = $2,
			    updated_at = NOW()
			WHERE id = $3
			  AND enabled = true
			RETURNING ` + sipTrunkSelectColumns
	} else {
		updateSQL = `
			UPDATE sip_trunks
			SET sip_auto_register = true,
			    lease_owner = $1,
			    lease_until = $2,
			    updated_at = NOW()
			WHERE id = $3
			  AND enabled = true
			  AND (lease_until IS NULL OR lease_until < NOW() OR lease_owner = $1)
			RETURNING ` + sipTrunkSelectColumns
	}

	trunk := &Trunk{}
	err := scanTrunkRow(tm.db.QueryRow(ctx, updateSQL, tm.instanceID, leaseUntil, trunkID), trunk)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			existing, getErr := tm.getTrunkByIDFromDB(ctx, trunkID)
			if getErr != nil {
				return getErr
			}
			if !existing.Enabled {
				return fmt.Errorf("trunk %d is disabled", trunkID)
			}
			if effectiveForce {
				return fmt.Errorf("trunk %d not found or disabled", trunkID)
			}
			return fmt.Errorf("%w: trunk %d lease held by another instance or trunk disabled", ErrTrunkLeaseLost, trunkID)
		}
		if effectiveForce {
			return fmt.Errorf("force lease update failed: %w", err)
		}
		return fmt.Errorf("%w: lease update failed for trunk %d on instance %s: %v", ErrTrunkLeaseRetry, trunkID, tm.instanceID, err)
	}

	tm.mu.Lock()
	if tm.ownedLeases == nil {
		tm.ownedLeases = make(map[int64]bool)
	}
	previous := tm.trunks[trunkID]
	wasOwned := tm.ownedLeases[trunkID]
	tm.setTrunkLocked(trunk)
	tm.ownedLeases[trunkID] = true
	delete(tm.leaseRetryRuns, trunkID)
	if trunkRegistrarIdentityChanged(previous, trunk) {
		tm.invalidateRegistrarIdentityLocked(trunkID)
	}
	if trunkRegistrationConfigChanged(previous, trunk) {
		tm.bumpTrunkConfigGenLocked(trunkID)
	}
	tm.mu.Unlock()

	if force {
		fmt.Printf("📞 [TrunkManager] Force-acquired lease for trunk %d (until %s)\n", trunkID, leaseUntil.Format(time.RFC3339))
	} else {
		if !wasOwned {
			tm.emitTrunkListChange("lease_updated", trunkID)
		}
		tm.logTrunkInfoOrDebug(!wasOwned, "📞 [TrunkManager] Acquired lease for trunk %d (until %s)\n", trunkID, leaseUntil.Format(time.RFC3339))
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

	// Persist logout intent and load trunk in a single statement before releasing
	// SIP so a refresh/restart cannot register the trunk again if later cleanup fails.
	trunk := &Trunk{}
	err := scanTrunkRow(tm.db.QueryRow(ctx, `
		UPDATE sip_trunks
		SET sip_auto_register = false, updated_at = NOW()
		WHERE id = $1
		RETURNING `+sipTrunkSelectColumns+`
	`, trunkID), trunk)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("trunk %d not found", trunkID)
		}
		return fmt.Errorf("disable automatic registration: %w", err)
	}
	trunk.SipAutoRegister = false

	// Stop refresh worker and clear registration cache
	tm.mu.Lock()
	if stopCh, exists := tm.refreshWorkers[trunkID]; exists {
		close(stopCh)
		delete(tm.refreshWorkers, trunkID)
	}
	tm.invalidateRegistrarIdentityLocked(trunkID)
	delete(tm.ownedLeases, trunkID)
	delete(tm.leaseRetryRuns, trunkID)
	delete(tm.registerRetries, trunkID)
	delete(tm.needsReRegister, trunkID)
	delete(tm.registeredPushContacts, trunkID)
	tm.setRegistrationAnnouncedLocked(trunkID, false)
	tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
		t.SipAutoRegister = false
		t.LeaseOwner = nil
		t.LeaseUntil = nil
	})
	tm.mu.Unlock()

	unregisterErr := tm.releaseTrunkContact(trunk)
	return tm.finalizeUnregisterInDB(trunkID, force, unregisterErr)
}

func (tm *TrunkManager) finalizeUnregisterInDB(trunkID int64, force bool, unregisterErr error) error {
	ctx, cancel := tm.dbContext()
	defer cancel()

	now := time.Now()
	var dbErr error
	if unregisterErr != nil {
		errMsg := unregisterErr.Error()
		if force {
			_, dbErr = tm.db.Exec(ctx, `
				UPDATE sip_trunks
				SET lease_owner = NULL,
				    lease_until = NULL,
				    last_error = $1,
				    updated_at = NOW()
				WHERE id = $2
			`, errMsg, trunkID)
		} else {
			_, dbErr = tm.db.Exec(ctx, `
				UPDATE sip_trunks
				SET lease_owner = CASE WHEN lease_owner = $3 THEN NULL ELSE lease_owner END,
				    lease_until = CASE WHEN lease_owner = $3 THEN NULL ELSE lease_until END,
				    last_error = $1,
				    updated_at = NOW()
				WHERE id = $2
			`, errMsg, trunkID, tm.instanceID)
		}
		if dbErr != nil {
			fmt.Printf("⚠️ [TrunkManager] Failed to update unregister error/lease for trunk %d: %v\n", trunkID, dbErr)
		}

		tm.mu.Lock()
		delete(tm.ownedLeases, trunkID)
		delete(tm.leaseRetryRuns, trunkID)
		tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
			msg := errMsg
			t.LeaseOwner = nil
			t.LeaseUntil = nil
			t.LastError = &msg
		})
		tm.setRegistrationAnnouncedLocked(trunkID, false)
		tm.mu.Unlock()

		tm.emitTrunkListChange("lease_updated", trunkID)
		tm.emitTrunkListChange("registration_updated", trunkID)
		return unregisterErr
	}

	if force {
		_, dbErr = tm.db.Exec(ctx, `
			UPDATE sip_trunks
			SET lease_owner = NULL,
			    lease_until = NULL,
			    last_registered_at = NULL,
			    last_unregistered_at = $1,
			    last_error = NULL,
			    sip_auto_register = false,
			    updated_at = NOW()
			WHERE id = $2
		`, now, trunkID)
	} else {
		_, dbErr = tm.db.Exec(ctx, `
			UPDATE sip_trunks
			SET lease_owner = CASE WHEN lease_owner = $3 THEN NULL ELSE lease_owner END,
			    lease_until = CASE WHEN lease_owner = $3 THEN NULL ELSE lease_until END,
			    last_registered_at = NULL,
			    last_unregistered_at = $1,
			    last_error = NULL,
			    sip_auto_register = false,
			    updated_at = NOW()
			WHERE id = $2
		`, now, trunkID, tm.instanceID)
	}
	if dbErr != nil {
		fmt.Printf("⚠️ [TrunkManager] Failed to release lease and clear registration status for trunk %d: %v\n", trunkID, dbErr)
		tm.emitTrunkListChange("lease_updated", trunkID)
		return fmt.Errorf("persist unregistered status: %w", dbErr)
	}

	if force {
		fmt.Printf("📞 [TrunkManager] Released lease for trunk %d (force)\n", trunkID)
	} else {
		fmt.Printf("📞 [TrunkManager] Released lease for trunk %d\n", trunkID)
	}

	tm.mu.Lock()
	delete(tm.ownedLeases, trunkID)
	delete(tm.leaseRetryRuns, trunkID)
	tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
		unregAt := now
		t.LeaseOwner = nil
		t.LeaseUntil = nil
		t.LastRegisteredAt = nil
		t.LastUnregisteredAt = &unregAt
		t.LastError = nil
		t.SipAutoRegister = false
	})
	tm.setRegistrationAnnouncedLocked(trunkID, false)
	tm.mu.Unlock()

	tm.emitTrunkListChange("lease_updated", trunkID)
	return nil
}

// GetTrunkByIDFromDB returns a single trunk by ID from the database.
func (tm *TrunkManager) GetTrunkByIDFromDB(ctx context.Context, trunkID int64) (*Trunk, error) {
	return tm.getTrunkByIDFromDB(ctx, trunkID)
}

func (tm *TrunkManager) queryTrunkByIDFromDB(ctx context.Context, trunkID int64) (*Trunk, error) {
	if tm.db == nil {
		return nil, fmt.Errorf("database not available for trunk manager")
	}

	dbCtx, cancel := dbContextWithTimeout(ctx)
	defer cancel()

	trunk := &Trunk{}
	err := scanTrunkRow(tm.db.QueryRow(dbCtx, `
		SELECT `+sipTrunkSelectColumns+`
		FROM sip_trunks
		WHERE id = $1
	`, trunkID), trunk)
	if err != nil {
		return nil, err
	}
	return trunk, nil
}

func (tm *TrunkManager) getTrunkByIDFromDB(ctx context.Context, trunkID int64) (*Trunk, error) {
	trunk, err := tm.queryTrunkByIDFromDB(ctx, trunkID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("trunk %d not found", trunkID)
		}
		return nil, fmt.Errorf("query failed: %w", err)
	}
	return trunk, nil
}

// GetTrunksByIDsFromDB fetches all matching trunks in a single SQL query (`WHERE id = ANY($1)`).
// Matching trunks are returned in the order of unique positive IDs requested.
func (tm *TrunkManager) GetTrunksByIDsFromDB(ctx context.Context, trunkIDs []int64) ([]*Trunk, error) {
	seen := make(map[int64]struct{}, len(trunkIDs))
	uniqueIDs := make([]int64, 0, len(trunkIDs))
	for _, id := range trunkIDs {
		if id <= 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) == 0 {
		return []*Trunk{}, nil
	}
	if tm.db == nil {
		return nil, fmt.Errorf("database not available for trunk manager")
	}

	dbCtx, cancel := dbContextWithTimeout(ctx)
	defer cancel()

	rows, err := tm.db.Query(dbCtx, `
		SELECT `+sipTrunkSelectColumns+`
		FROM sip_trunks
		WHERE id = ANY($1)
	`, uniqueIDs)
	if err != nil {
		return nil, fmt.Errorf("batch trunk query failed: %w", err)
	}
	defer rows.Close()

	byID := make(map[int64]*Trunk, len(uniqueIDs))
	for rows.Next() {
		trunk := &Trunk{}
		if err := scanTrunkRow(rows, trunk); err != nil {
			return nil, fmt.Errorf("batch trunk scan failed: %w", err)
		}
		byID[trunk.ID] = trunk
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("batch trunk rows iteration failed: %w", err)
	}

	ordered := make([]*Trunk, 0, len(byID))
	for _, id := range uniqueIDs {
		if trunk, ok := byID[id]; ok {
			ordered = append(ordered, trunk)
		}
	}
	return ordered, nil
}

// ValidateAndClaimTrunkForOutboundCall validates that trunkID has an active lease on
// instanceID and, when claimUsername is non-nil, atomically sets in_use_by in a single
// UPDATE ... RETURNING query. On 0 matched rows it falls back to reading the trunk so
// callers receive exact notFound / lease-owner diagnostics.
func (tm *TrunkManager) ValidateAndClaimTrunkForOutboundCall(
	ctx context.Context,
	trunkID int64,
	instanceID string,
	claimUsername *string,
) (trunk *Trunk, notFound bool, notReadyReason string, err error) {
	if tm == nil {
		return nil, false, "", fmt.Errorf("database not available for trunk manager")
	}

	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		instanceID = tm.instanceID
	}

	if tm.IsSingleInstance() {
		tm.mu.Lock()
		cached, ok := tm.trunks[trunkID]
		if ok && cached != nil {
			if !cached.Enabled {
				tm.mu.Unlock()
				return nil, false, fmt.Sprintf("Trunk %d is disabled", cached.ID), nil
			}
			if claimUsername != nil {
				claimedCopy := *claimUsername
				updated := tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
					t.InUseBy = &claimedCopy
				})
				gen := tm.bumpInUseByGenLocked(trunkID)
				tm.mu.Unlock()
				tm.emitTrunkListChange("usage_updated", trunkID)
				tm.enqueueAsyncInUseByUpdate(trunkID, &claimedCopy, gen)
				return updated, false, "", nil
			}
			tm.mu.Unlock()
			return cached, false, "", nil
		}
		tm.mu.Unlock()

		// Trunk missing from RAM in single-instance mode: fetch from DB without
		// requiring DB lease_owner/lease_until, cache locally, and claim if requested.
		if tm.db == nil {
			return nil, false, "", fmt.Errorf("database not available for trunk manager")
		}
		dbCtx, cancel := dbContextWithTimeout(ctx)
		defer cancel()

		existing, queryErr := tm.queryTrunkByIDFromDB(dbCtx, trunkID)
		if queryErr != nil {
			if errors.Is(queryErr, pgx.ErrNoRows) {
				return nil, true, fmt.Sprintf("Trunk %d not found", trunkID), nil
			}
			return nil, false, "", fmt.Errorf("query failed: %w", queryErr)
		}
		if !existing.Enabled {
			return nil, false, fmt.Sprintf("Trunk %d is disabled", existing.ID), nil
		}

		lo := tm.instanceID
		var claimedCopy *string
		if claimUsername != nil {
			c := *claimUsername
			claimedCopy = &c
		}

		tm.mu.Lock()
		if cached, exists := tm.trunks[trunkID]; exists && cached != nil {
			if !cached.Enabled {
				tm.mu.Unlock()
				return nil, false, fmt.Sprintf("Trunk %d is disabled", cached.ID), nil
			}
		} else {
			toCache := cloneTrunk(existing)
			toCache.LeaseOwner = &lo
			tm.setTrunkLocked(toCache)
		}
		if tm.ownedLeases == nil {
			tm.ownedLeases = make(map[int64]bool)
		}
		tm.ownedLeases[trunkID] = true

		var result *Trunk
		var gen uint64
		if claimedCopy != nil {
			result = tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
				t.LeaseOwner = &lo
				t.InUseBy = cloneStringPtr(claimedCopy)
			})
			gen = tm.bumpInUseByGenLocked(trunkID)
		} else {
			result = tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
				t.LeaseOwner = &lo
			})
		}
		tm.mu.Unlock()

		if claimedCopy != nil {
			tm.emitTrunkListChange("usage_updated", trunkID)
			tm.enqueueAsyncInUseByUpdate(trunkID, claimedCopy, gen)
		}
		return result, false, "", nil
	}

	if tm.db == nil {
		return nil, false, "", fmt.Errorf("database not available for trunk manager")
	}

	dbCtx, cancel := dbContextWithTimeout(ctx)
	defer cancel()

	if claimUsername != nil {
		claimed, claimErr := tm.atomicClaimOutboundTrunk(dbCtx, trunkID, instanceID, claimUsername)
		if claimErr == nil {
			return claimed, false, "", nil
		}
		if !errors.Is(claimErr, pgx.ErrNoRows) {
			return nil, false, "", fmt.Errorf("validate and claim trunk failed: %w", claimErr)
		}

		// 0 rows matched: inspect current DB state to return exact notFound / lease reason.
		existing, queryErr := tm.queryTrunkByIDFromDB(dbCtx, trunkID)
		if queryErr != nil {
			if errors.Is(queryErr, pgx.ErrNoRows) {
				return nil, true, fmt.Sprintf("Trunk %d not found", trunkID), nil
			}
			return nil, false, "", fmt.Errorf("query failed: %w", queryErr)
		}
		if reason := validateTrunkLeaseForOutbound(existing, instanceID, time.Now()); reason != "" {
			return nil, false, reason, nil
		}
		// Rare race: lease became valid between UPDATE and fallback SELECT; retry claim once.
		claimed, retryErr := tm.atomicClaimOutboundTrunk(dbCtx, trunkID, instanceID, claimUsername)
		if retryErr == nil {
			return claimed, false, "", nil
		}
		if errors.Is(retryErr, pgx.ErrNoRows) {
			return nil, false, "Trunk lease not active", nil
		}
		return nil, false, "", fmt.Errorf("validate and claim trunk failed: %w", retryErr)
	}

	existing, queryErr := tm.queryTrunkByIDFromDB(dbCtx, trunkID)
	if queryErr != nil {
		if errors.Is(queryErr, pgx.ErrNoRows) {
			return nil, true, fmt.Sprintf("Trunk %d not found", trunkID), nil
		}
		return nil, false, "", fmt.Errorf("query failed: %w", queryErr)
	}
	if reason := validateTrunkLeaseForOutbound(existing, instanceID, time.Now()); reason != "" {
		return nil, false, reason, nil
	}
	return existing, false, "", nil
}

func (tm *TrunkManager) atomicClaimOutboundTrunk(ctx context.Context, trunkID int64, instanceID string, claimUsername *string) (*Trunk, error) {
	trunk := &Trunk{}
	err := scanTrunkRow(tm.db.QueryRow(ctx, `
		UPDATE sip_trunks
		SET in_use_by = $1, updated_at = NOW()
		WHERE id = $2
		  AND enabled = true
		  AND lease_owner = $3
		  AND lease_until > NOW()
		RETURNING `+sipTrunkSelectColumns+`
	`, claimUsername, trunkID, instanceID), trunk)
	if err != nil {
		return nil, err
	}

	claimedCopy := *claimUsername
	tm.mu.Lock()
	tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
		claimed := claimedCopy
		t.InUseBy = &claimed
	})
	tm.mu.Unlock()

	tm.emitTrunkListChange("usage_updated", trunkID)
	return trunk, nil
}

func validateTrunkLeaseForOutbound(trunk *Trunk, instanceID string, now time.Time) string {
	if trunk == nil {
		return "Trunk lease not active"
	}
	if trunk.LeaseOwner == nil || strings.TrimSpace(*trunk.LeaseOwner) == "" || trunk.LeaseUntil == nil || trunk.LeaseUntil.Before(now) {
		return "Trunk lease not active"
	}
	leaseOwner := strings.TrimSpace(*trunk.LeaseOwner)
	if leaseOwner != instanceID {
		return fmt.Sprintf("Trunk is owned by another gateway instance (%s)", leaseOwner)
	}
	if !trunk.Enabled {
		return fmt.Sprintf("Trunk %d is disabled", trunk.ID)
	}
	return ""
}

// SetTrunkInUseBy sets or clears the in_use_by field for a trunk.
// Pass a non-nil username pointer to set it, nil to clear it.
func (tm *TrunkManager) SetTrunkInUseBy(ctx context.Context, trunkID int64, username *string) error {
	if tm.db == nil {
		return fmt.Errorf("database not available for trunk manager")
	}

	if tm.IsSingleInstance() {
		desired := cloneStringPtr(username)
		tm.mu.Lock()
		tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
			t.InUseBy = cloneStringPtr(desired)
		})
		myGen := tm.bumpInUseByGenLocked(trunkID)
		tm.mu.Unlock()

		tm.emitTrunkListChange("usage_updated", trunkID)

		writeMu := tm.getInUseByWriteMu(trunkID)
		writeMu.Lock()
		defer writeMu.Unlock()

		tm.mu.RLock()
		currentGen := tm.inUseByGen[trunkID]
		flushedGen := uint64(0)
		if tm.inUseByFlushedGen != nil {
			flushedGen = tm.inUseByFlushedGen[trunkID]
		}
		if currentGen != 0 && flushedGen >= currentGen {
			tm.mu.RUnlock()
			return nil
		}
		valToWrite := desired
		targetGen := myGen
		if currentGen > targetGen {
			targetGen = currentGen
		}
		if trunk, ok := tm.trunks[trunkID]; ok && trunk != nil {
			valToWrite = cloneStringPtr(trunk.InUseBy)
		}
		tm.mu.RUnlock()

		dbCtx, cancel := dbContextWithTimeout(ctx)
		defer cancel()

		_, err := tm.db.Exec(dbCtx, `
			UPDATE sip_trunks SET in_use_by = $1, updated_at = NOW() WHERE id = $2
		`, valToWrite, trunkID)
		if err != nil {
			return fmt.Errorf("set in_use_by failed: %w", err)
		}

		tm.mu.Lock()
		if tm.inUseByFlushedGen == nil {
			tm.inUseByFlushedGen = make(map[int64]uint64)
		}
		if targetGen > tm.inUseByFlushedGen[trunkID] {
			tm.inUseByFlushedGen[trunkID] = targetGen
		}
		tm.mu.Unlock()

		return nil
	}

	dbCtx, cancel := dbContextWithTimeout(ctx)
	defer cancel()

	_, err := tm.db.Exec(dbCtx, `
		UPDATE sip_trunks SET in_use_by = $1, updated_at = NOW() WHERE id = $2
	`, username, trunkID)
	if err != nil {
		return fmt.Errorf("set in_use_by failed: %w", err)
	}

	tm.mu.Lock()
	tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
		t.InUseBy = cloneStringPtr(username)
	})
	tm.mu.Unlock()

	tm.emitTrunkListChange("usage_updated", trunkID)
	return nil
}

// SetTrunkNotifyUserID sets or clears the notify_user_id field for a trunk.
// This stores the Keycloak sub (UUID) used for push notifications.
// Unlike InUseBy, this is NOT cleared on hangup — it persists so push works when offline.
func (tm *TrunkManager) SetTrunkNotifyUserID(ctx context.Context, trunkID int64, userID *string) error {
	return tm.setTrunkNotifyUserIDAndPlatform(ctx, trunkID, userID, nil)
}

// SetTrunkNotifyUserIDAndPlatform sets notify_user_id and, when provided, the latest mobile platform atomically.
func (tm *TrunkManager) SetTrunkNotifyUserIDAndPlatform(ctx context.Context, trunkID int64, userID *string, platform *string) error {
	return tm.setTrunkNotifyUserIDAndPlatform(ctx, trunkID, userID, platform)
}

func (tm *TrunkManager) setTrunkNotifyUserIDAndPlatform(ctx context.Context, trunkID int64, userID *string, platform *string) error {
	if tm.db == nil {
		return fmt.Errorf("database not available for trunk manager")
	}

	var normalizedUserID *string
	if userID != nil {
		trimmed := strings.TrimSpace(*userID)
		if trimmed != "" {
			normalizedUserID = &trimmed
		}
	}

	var normalizedPlatform *string
	if platform != nil {
		trimmed := strings.ToLower(strings.TrimSpace(*platform))
		if trimmed != "" {
			normalizedPlatform = &trimmed
		}
	}

	dbCtx, cancel := dbContextWithTimeout(ctx)
	defer cancel()

	var targetUsername, targetDomain string
	var targetPort int
	var err error

	switch {
	case normalizedUserID != nil && normalizedPlatform != nil:
		err = tm.db.QueryRow(dbCtx, `
			WITH target AS (
				SELECT id, username, domain, port
				FROM sip_trunks
				WHERE id = $1
				FOR UPDATE
			),
			cleared AS (
				UPDATE sip_trunks s
				SET notify_user_id = NULL, updated_at = NOW()
				FROM target t
				WHERE s.notify_user_id = $2
				  AND s.id <> t.id
				  AND (s.username <> t.username OR s.domain <> t.domain OR s.port <> t.port)
				RETURNING s.id
			),
			updated AS (
				UPDATE sip_trunks s
				SET notify_user_id = $2,
				    last_online_platform = $3,
				    last_online_at = NOW(),
				    updated_at = NOW()
				FROM target t
				WHERE s.id = t.id
				RETURNING t.username, t.domain, t.port
			)
			SELECT username, domain, port FROM updated
		`, trunkID, *normalizedUserID, *normalizedPlatform).Scan(&targetUsername, &targetDomain, &targetPort)
	case normalizedUserID != nil:
		err = tm.db.QueryRow(dbCtx, `
			WITH target AS (
				SELECT id, username, domain, port
				FROM sip_trunks
				WHERE id = $1
				FOR UPDATE
			),
			cleared AS (
				UPDATE sip_trunks s
				SET notify_user_id = NULL, updated_at = NOW()
				FROM target t
				WHERE s.notify_user_id = $2
				  AND s.id <> t.id
				  AND (s.username <> t.username OR s.domain <> t.domain OR s.port <> t.port)
				RETURNING s.id
			),
			updated AS (
				UPDATE sip_trunks s
				SET notify_user_id = $2,
				    updated_at = NOW()
				FROM target t
				WHERE s.id = t.id
				RETURNING t.username, t.domain, t.port
			)
			SELECT username, domain, port FROM updated
		`, trunkID, *normalizedUserID).Scan(&targetUsername, &targetDomain, &targetPort)
	case normalizedPlatform != nil:
		err = tm.db.QueryRow(dbCtx, `
			UPDATE sip_trunks
			SET notify_user_id = NULL,
			    last_online_platform = $1,
			    last_online_at = NOW(),
			    updated_at = NOW()
			WHERE id = $2
			RETURNING username, domain, port
		`, *normalizedPlatform, trunkID).Scan(&targetUsername, &targetDomain, &targetPort)
	default:
		err = tm.db.QueryRow(dbCtx, `
			UPDATE sip_trunks
			SET notify_user_id = NULL, updated_at = NOW()
			WHERE id = $1
			RETURNING username, domain, port
		`, trunkID).Scan(&targetUsername, &targetDomain, &targetPort)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: trunk %d", ErrTrunkNotFound, trunkID)
		}
		return fmt.Errorf("set notify_user_id failed: %w", err)
	}

	tm.mu.Lock()
	tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
		t.NotifyUserID = cloneStringPtr(normalizedUserID)
		if normalizedPlatform != nil {
			now := time.Now()
			t.LastOnlinePlatform = cloneStringPtr(normalizedPlatform)
			t.LastOnlineAt = &now
		}
	})
	bindingIDs := []int64{trunkID}
	if normalizedUserID != nil {
		for id, trunk := range tm.trunks {
			if id == trunkID || trunk == nil || trunk.NotifyUserID == nil || *trunk.NotifyUserID != *normalizedUserID {
				continue
			}
			if trunk.Username != targetUsername || trunk.Domain != targetDomain || trunk.Port != targetPort {
				tm.updateTrunkCoWLocked(id, func(t *Trunk) {
					t.NotifyUserID = nil
				})
				bindingIDs = append(bindingIDs, id)
			}
		}
	}
	tm.mu.Unlock()

	tm.emitTrunkListChange("binding_updated", bindingIDs...)
	return nil
}

// SetTrunkPushContact stores the latest SIP Contact push parameters for a trunk.
// It returns true when the effective stored contact changed.
func (tm *TrunkManager) SetTrunkPushContact(ctx context.Context, trunkID int64, contact TrunkPushContact) (bool, error) {
	if tm.db == nil {
		return false, fmt.Errorf("database not available for trunk manager")
	}

	appID := strings.TrimSpace(contact.PNAppID)
	pnType := strings.TrimSpace(contact.PNType)
	pnToken := strings.TrimSpace(contact.PNToken)
	if appID == "" || pnType == "" || pnToken == "" {
		return false, fmt.Errorf("%w: push contact requires app-id, pn-type, and pn-token", ErrTrunkValidation)
	}
	normalizedContact := TrunkPushContact{
		PNAppID: appID,
		PNType:  pnType,
		PNToken: pnToken,
	}

	now := time.Now()
	dbCtx, cancel := dbContextWithTimeout(ctx)
	defer cancel()

	var changed bool
	err := tm.db.QueryRow(dbCtx, `
		WITH target AS (
			SELECT id, pn_app_id, pn_type, pn_token
			FROM sip_trunks
			WHERE id = $5
			  AND enabled = true
			FOR UPDATE
		),
		updated AS (
			UPDATE sip_trunks s
			SET pn_app_id = $1,
			    pn_type = $2,
			    pn_token = $3,
			    pn_updated_at = $4,
			    updated_at = NOW()
			FROM target t
			WHERE s.id = t.id
			  AND (
				COALESCE(btrim(t.pn_app_id), '') <> $1
				OR COALESCE(btrim(t.pn_type), '') <> $2
				OR COALESCE(btrim(t.pn_token), '') <> $3
			  )
			RETURNING s.id
		)
		SELECT EXISTS(SELECT 1 FROM updated) AS changed
		FROM target
	`, appID, pnType, pnToken, now, trunkID).Scan(&changed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("%w: trunk %d", ErrTrunkNotFound, trunkID)
		}
		return false, fmt.Errorf("set trunk push contact failed: %w", err)
	}

	tm.mu.Lock()
	tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
		app := normalizedContact.PNAppID
		typ := normalizedContact.PNType
		tok := normalizedContact.PNToken
		t.PNAppID = &app
		t.PNType = &typ
		t.PNToken = &tok
		if changed {
			updatedAt := now
			t.PNUpdatedAt = &updatedAt
		}
	})
	if changed {
		if tm.needsReRegister == nil {
			tm.needsReRegister = make(map[int64]bool)
		}
		tm.needsReRegister[trunkID] = true
		tm.bumpTrunkConfigGenLocked(trunkID)
	}
	tm.mu.Unlock()

	return changed, nil
}

func (tm *TrunkManager) SetTrunkFcmToken(ctx context.Context, trunkID int64, token string) error {
	if tm.db == nil {
		return fmt.Errorf("database not available for trunk manager")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("%w: fcm token is required", ErrTrunkValidation)
	}
	now := time.Now()
	dbCtx, cancel := dbContextWithTimeout(ctx)
	defer cancel()
	result, err := tm.db.Exec(dbCtx, `
		UPDATE sip_trunks
		SET fcm_token = $1, fcm_updated_at = $2, updated_at = NOW()
		WHERE id = $3 AND enabled = true
	`, token, now, trunkID)
	if err != nil {
		return fmt.Errorf("set trunk fcm token failed: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("%w: trunk %d", ErrTrunkNotFound, trunkID)
	}
	tm.mu.Lock()
	tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
		tok := token
		updatedAt := now
		t.FcmToken = &tok
		t.FcmUpdatedAt = &updatedAt
	})
	tm.mu.Unlock()
	tm.emitTrunkListChange("push_updated", trunkID)
	return nil
}

func (tm *TrunkManager) ClearTrunkFcmToken(ctx context.Context, trunkID int64) error {
	if tm.db == nil {
		return fmt.Errorf("database not available for trunk manager")
	}
	dbCtx, cancel := dbContextWithTimeout(ctx)
	defer cancel()
	_, err := tm.db.Exec(dbCtx, `
		UPDATE sip_trunks
		SET fcm_token = NULL, fcm_updated_at = NULL, updated_at = NOW()
		WHERE id = $1
	`, trunkID)
	if err != nil {
		return fmt.Errorf("clear trunk fcm token failed: %w", err)
	}
	tm.mu.Lock()
	tm.updateTrunkCoWLocked(trunkID, func(t *Trunk) {
		t.FcmToken = nil
		t.FcmUpdatedAt = nil
	})
	tm.mu.Unlock()
	tm.emitTrunkListChange("push_updated", trunkID)
	return nil
}

func (tm *TrunkManager) ListAgentDeviceTrunksByNotifyUserID(ctx context.Context, userID string) ([]*Trunk, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, nil
	}
	if tm.db == nil {
		tm.mu.RLock()
		defer tm.mu.RUnlock()
		out := make([]*Trunk, 0)
		for _, trunk := range tm.trunks {
			if trunk == nil || !IsAgentDeviceTrunkName(trunk.Name) || trunk.NotifyUserID == nil {
				continue
			}
			if strings.TrimSpace(*trunk.NotifyUserID) == userID {
				out = append(out, cloneTrunk(trunk))
			}
		}
		return out, nil
	}
	dbCtx, cancel := dbContextWithTimeout(ctx)
	defer cancel()
	rows, err := tm.db.Query(dbCtx, `
		SELECT `+sipTrunkSelectColumns+`
		FROM sip_trunks
		WHERE notify_user_id = $1 AND name LIKE 'sipclient-agent-device-%'
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list agent-device trunks failed: %w", err)
	}
	defer rows.Close()
	out := make([]*Trunk, 0)
	for rows.Next() {
		trunk := &Trunk{}
		if err := scanTrunkRow(rows, trunk); err != nil {
			return nil, fmt.Errorf("scan agent-device trunk failed: %w", err)
		}
		out = append(out, trunk)
	}
	return out, rows.Err()
}

func (tm *TrunkManager) IdentityGroupTrunkIDs(trunkID int64) []int64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	source := tm.trunks[trunkID]
	if source == nil {
		return nil
	}
	user := strings.TrimSpace(source.Username)
	domain := normalizeSIPHost(source.Domain)
	port := normalizeSIPPort(source.Port)
	ids := make([]int64, 0)
	if tm.trunksByIdentity != nil {
		key := makeTrunkIdentityKey(source.Username, source.Domain, source.Port)
		for _, id := range tm.trunksByIdentity[key] {
			if id == trunkID || !tm.ownedLeases[id] {
				continue
			}
			trunk := tm.trunks[id]
			if trunk == nil {
				continue
			}
			if strings.TrimSpace(trunk.Username) != user || normalizeSIPHost(trunk.Domain) != domain || normalizeSIPPort(trunk.Port) != port {
				continue
			}
			if IsAgentTrunkName(trunk.Name) || IsAgentDeviceTrunkName(trunk.Name) {
				ids = append(ids, id)
			}
		}
		return ids
	}
	// Fallback when tm.trunks was populated directly in unit tests without index init.
	for id, trunk := range tm.trunks {
		if trunk == nil || id == trunkID || !tm.ownedLeases[id] {
			continue
		}
		if strings.TrimSpace(trunk.Username) != user || normalizeSIPHost(trunk.Domain) != domain || normalizeSIPPort(trunk.Port) != port {
			continue
		}
		if IsAgentTrunkName(trunk.Name) || IsAgentDeviceTrunkName(trunk.Name) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func trimStringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// FindTrunkByInUseBy finds a trunk currently assigned to the given in_use_by value.
// Returns (nil, nil) if no trunk is found.
func (tm *TrunkManager) FindTrunkByInUseBy(ctx context.Context, inUseBy string) (*Trunk, error) {
	if tm == nil {
		return nil, fmt.Errorf("database not available for trunk manager")
	}
	singleInstance := tm.IsSingleInstance()
	if singleInstance {
		tm.mu.RLock()
		var matched *Trunk
		for _, trunk := range tm.trunks {
			if trunk == nil || trunk.InUseBy == nil {
				continue
			}
			if *trunk.InUseBy == inUseBy {
				if matched == nil || trunk.ID < matched.ID {
					matched = trunk
				}
			}
		}
		tm.mu.RUnlock()
		if matched != nil {
			return matched, nil
		}
		if tm.db == nil {
			return nil, nil
		}
	}

	if tm.db == nil {
		return nil, fmt.Errorf("database not available for trunk manager")
	}

	dbCtx, cancel := dbContextWithTimeout(ctx)
	defer cancel()

	trunk := &Trunk{}
	err := scanTrunkRow(tm.db.QueryRow(dbCtx, `
		SELECT `+sipTrunkSelectColumns+`
		FROM sip_trunks
		WHERE in_use_by = $1
		LIMIT 1
	`, inUseBy), trunk)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find trunk by in_use_by failed: %w", err)
	}

	if singleInstance {
		tm.mu.RLock()
		cached, inRAM := tm.trunks[trunk.ID]
		tm.mu.RUnlock()
		if inRAM && cached != nil {
			if cached.InUseBy == nil || *cached.InUseBy != inUseBy {
				return nil, nil
			}
			return cached, nil
		}
	}

	return trunk, nil
}

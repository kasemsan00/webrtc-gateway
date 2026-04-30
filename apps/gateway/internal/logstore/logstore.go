package logstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"k2-gateway/internal/config"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LogStore interface for database logging
type LogStore interface {
	// Lifecycle
	Start(ctx context.Context) error
	Stop() error

	// Session management
	UpsertSession(ctx context.Context, sess *SessionRecord) error

	// Event logging (async via queue)
	LogEvent(event *Event)

	// Payload storage (sync - returns payloadID)
	StorePayload(ctx context.Context, payload *PayloadRecord) (int64, error)

	// Stats recording (async via queue)
	RecordStats(stats *StatsRecord)

	// Dialog state
	UpsertDialog(ctx context.Context, dialog *DialogRecord) error

	// Session directory (for resume/redirect across instances)
	UpsertSessionDirectory(ctx context.Context, sessionID, ownerInstanceID, wsURL string, ttlSeconds int) error
	LookupSessionDirectory(ctx context.Context, sessionID string) (ownerInstanceID, wsURL string, found bool, err error)
	DeleteSessionDirectory(ctx context.Context, sessionID string) error

	// Gateway instance registry (for trunk redirect)
	UpsertGatewayInstance(ctx context.Context, instanceID, wsURL string, ttlSeconds int) error
	LookupGatewayInstance(ctx context.Context, instanceID string) (wsURL string, found bool, err error)

	// Trunk resolve by credentials
	ResolveTrunkByCredentials(ctx context.Context, domain string, port int, username, password string) (trunkID int64, leaseOwner *string, leaseUntil *time.Time, found bool, err error)
	ResolveOrCreateTrunk(ctx context.Context, domain string, port int, username, password, transport, instanceID string, leaseTTLSeconds int) (trunkID int64, leaseOwner *string, leaseUntil *time.Time, created bool, err error)

	// Session history (paginated)
	ListSessions(ctx context.Context, params SessionListParams) (*SessionListResult, error)

	// Session detail queries
	ListEvents(ctx context.Context, params EventListParams) (*EventListResult, error)
	ListPayloads(ctx context.Context, params PayloadListParams) (*PayloadListResult, error)
	GetPayload(ctx context.Context, payloadID int64) (*PayloadReadRecord, error)
	ListDialogs(ctx context.Context, params DialogListParams) (*DialogListResult, error)
	ListStats(ctx context.Context, params StatsListParams) (*StatsListResult, error)

	// Ops queries
	ListGatewayInstances(ctx context.Context, params GatewayInstanceListParams) (*GatewayInstanceListResult, error)
	ListSessionDirectory(ctx context.Context, params SessionDirectoryListParams) (*SessionDirectoryListResult, error)
	GetDashboardSummary(ctx context.Context, params DashboardSummaryParams) (*DashboardSummaryResult, error)

	// DB access (for TrunkManager)
	GetDB() *pgxpool.Pool
}

// logStore implements LogStore interface
type logStore struct {
	config     config.DBConfig
	pool       *pgxpool.Pool
	eventQueue chan *Event
	statsQueue chan *StatsRecord
	wg         sync.WaitGroup
	stopCh     chan struct{}
}

// noopStore is a no-op implementation when DB is disabled
type noopStore struct{}

func (n *noopStore) Start(ctx context.Context) error                              { return nil }
func (n *noopStore) Stop() error                                                  { return nil }
func (n *noopStore) UpsertSession(ctx context.Context, sess *SessionRecord) error { return nil }
func (n *noopStore) LogEvent(event *Event)                                        {}
func (n *noopStore) StorePayload(ctx context.Context, payload *PayloadRecord) (int64, error) {
	return 0, nil
}
func (n *noopStore) RecordStats(stats *StatsRecord)                               {}
func (n *noopStore) UpsertDialog(ctx context.Context, dialog *DialogRecord) error { return nil }
func (n *noopStore) UpsertSessionDirectory(ctx context.Context, sessionID, ownerInstanceID, wsURL string, ttlSeconds int) error {
	return nil
}
func (n *noopStore) LookupSessionDirectory(ctx context.Context, sessionID string) (string, string, bool, error) {
	return "", "", false, nil
}
func (n *noopStore) DeleteSessionDirectory(ctx context.Context, sessionID string) error {
	return nil
}
func (n *noopStore) UpsertGatewayInstance(ctx context.Context, instanceID, wsURL string, ttlSeconds int) error {
	return nil
}
func (n *noopStore) LookupGatewayInstance(ctx context.Context, instanceID string) (string, bool, error) {
	return "", false, nil
}
func (n *noopStore) ResolveTrunkByCredentials(ctx context.Context, domain string, port int, username, password string) (int64, *string, *time.Time, bool, error) {
	return 0, nil, nil, false, nil
}
func (n *noopStore) ResolveOrCreateTrunk(ctx context.Context, domain string, port int, username, password, transport, instanceID string, leaseTTLSeconds int) (int64, *string, *time.Time, bool, error) {
	return 0, nil, nil, false, nil
}
func (n *noopStore) ListSessions(ctx context.Context, params SessionListParams) (*SessionListResult, error) {
	return &SessionListResult{Items: []*SessionRecord{}, Total: 0, Page: 1, PageSize: 20}, nil
}
func (n *noopStore) ListEvents(ctx context.Context, params EventListParams) (*EventListResult, error) {
	return &EventListResult{Items: []*EventRecord{}, Total: 0, Page: 1, PageSize: 20}, nil
}
func (n *noopStore) ListPayloads(ctx context.Context, params PayloadListParams) (*PayloadListResult, error) {
	return &PayloadListResult{Items: []*PayloadReadRecord{}, Total: 0, Page: 1, PageSize: 20}, nil
}
func (n *noopStore) GetPayload(ctx context.Context, payloadID int64) (*PayloadReadRecord, error) {
	return nil, fmt.Errorf("database not available")
}
func (n *noopStore) ListDialogs(ctx context.Context, params DialogListParams) (*DialogListResult, error) {
	return &DialogListResult{Items: []*DialogRecord{}, Total: 0, Page: 1, PageSize: 20}, nil
}
func (n *noopStore) ListStats(ctx context.Context, params StatsListParams) (*StatsListResult, error) {
	return &StatsListResult{Items: []*StatsReadRecord{}, Total: 0, Page: 1, PageSize: 20}, nil
}
func (n *noopStore) ListGatewayInstances(ctx context.Context, params GatewayInstanceListParams) (*GatewayInstanceListResult, error) {
	return &GatewayInstanceListResult{Items: []*GatewayInstanceRecord{}, Total: 0, Page: 1, PageSize: 20}, nil
}
func (n *noopStore) ListSessionDirectory(ctx context.Context, params SessionDirectoryListParams) (*SessionDirectoryListResult, error) {
	return &SessionDirectoryListResult{Items: []*SessionDirectoryRecord{}, Total: 0, Page: 1, PageSize: 20}, nil
}
func (n *noopStore) GetDashboardSummary(ctx context.Context, params DashboardSummaryParams) (*DashboardSummaryResult, error) {
	return &DashboardSummaryResult{
		TotalSessions:         0,
		SessionDirectoryCount: 0,
		Series:                []DashboardSeriesPoint{},
		States:                []DashboardStateCount{},
		TopTrunks:             []DashboardTrunkCount{},
	}, nil
}
func (n *noopStore) GetDB() *pgxpool.Pool {
	return nil
}

// New creates a new LogStore instance
func New(cfg config.DBConfig) (LogStore, error) {
	if !cfg.Enable {
		fmt.Printf("📊 Database logging: Disabled\n")
		return &noopStore{}, nil
	}

	return &logStore{
		config:     cfg,
		eventQueue: make(chan *Event, 10000),      // Buffer 10k events
		statsQueue: make(chan *StatsRecord, 1000), // Buffer 1k stats
		stopCh:     make(chan struct{}),
	}, nil
}

// Start initializes database connection and background workers
func (s *logStore) Start(ctx context.Context) error {
	// Parse DSN
	poolConfig, err := pgxpool.ParseConfig(s.config.DSN)
	if err != nil {
		return fmt.Errorf("failed to parse DSN: %w", err)
	}

	// Configure connection pool
	poolConfig.MaxConns = 10
	poolConfig.MinConns = 2
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute

	// Create pool
	s.pool, err = pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("failed to create pool: %w", err)
	}

	// Test connection
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	fmt.Printf("✅ Database connection established\n")
	fmt.Printf("📊 Database logging: Enabled\n")
	fmt.Printf("   - Batch size: %d events\n", s.config.BatchSize)
	fmt.Printf("   - Batch interval: %d ms\n", s.config.BatchIntervalMS)
	fmt.Printf("   - Stats interval: %d ms\n", s.config.StatsIntervalMS)
	fmt.Printf("   - Log full SIP: %v\n", s.config.LogFullSIP)

	// Start background workers
	s.wg.Add(5)
	go s.eventBatchWorker()
	go s.statsBatchWorker()
	go s.partitionMaintenanceWorker()
	go s.sessionDirectoryCleanupWorker()
	go s.gatewayInstanceCleanupWorker()

	return nil
}

// Stop gracefully stops the log store
func (s *logStore) Stop() error {
	fmt.Printf("🛑 Stopping log store...\n")

	// Signal workers to stop
	close(s.stopCh)

	// Wait for workers to finish
	s.wg.Wait()

	// Close database pool
	if s.pool != nil {
		s.pool.Close()
	}

	fmt.Printf("✅ Log store stopped\n")
	return nil
}

// UpsertSession inserts or updates a session record
func (s *logStore) UpsertSession(ctx context.Context, sess *SessionRecord) error {
	if s.pool == nil {
		return fmt.Errorf("database pool not initialized")
	}

	metaJSON, err := json.Marshal(sess.Meta)
	if err != nil {
		metaJSON = []byte("{}")
	}

	query := `
		INSERT INTO call_sessions (
			session_id, created_at, updated_at, ended_at,
			direction, from_uri, to_uri,
			sip_call_id, final_state, end_reason,
			rtp_audio_port, rtp_video_port, rtcp_audio_port, rtcp_video_port,
			sip_opus_pt, audio_profile, video_profile, video_rejected,
			auth_mode, trunk_id, trunk_name, sip_username,
			meta
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23
		)
		ON CONFLICT (session_id) DO UPDATE SET
			updated_at = EXCLUDED.updated_at,
			ended_at = EXCLUDED.ended_at,
			direction = COALESCE(NULLIF(EXCLUDED.direction, ''), call_sessions.direction),
			from_uri = COALESCE(NULLIF(EXCLUDED.from_uri, ''), call_sessions.from_uri),
			to_uri = COALESCE(NULLIF(EXCLUDED.to_uri, ''), call_sessions.to_uri),
			sip_call_id = COALESCE(NULLIF(EXCLUDED.sip_call_id, ''), call_sessions.sip_call_id),
			final_state = EXCLUDED.final_state,
			end_reason = EXCLUDED.end_reason,
			rtp_audio_port = EXCLUDED.rtp_audio_port,
			rtp_video_port = EXCLUDED.rtp_video_port,
			rtcp_audio_port = EXCLUDED.rtcp_audio_port,
			rtcp_video_port = EXCLUDED.rtcp_video_port,
			sip_opus_pt = EXCLUDED.sip_opus_pt,
			audio_profile = EXCLUDED.audio_profile,
			video_profile = EXCLUDED.video_profile,
			video_rejected = EXCLUDED.video_rejected,
			auth_mode = COALESCE(NULLIF(EXCLUDED.auth_mode, ''), call_sessions.auth_mode),
			trunk_id = EXCLUDED.trunk_id,
			trunk_name = COALESCE(NULLIF(EXCLUDED.trunk_name, ''), call_sessions.trunk_name),
			sip_username = COALESCE(NULLIF(EXCLUDED.sip_username, ''), call_sessions.sip_username),
			meta = EXCLUDED.meta
	`

	_, err = s.pool.Exec(ctx, query,
		sess.SessionID, sess.CreatedAt, sess.UpdatedAt, sess.EndedAt,
		sess.Direction, sess.FromURI, sess.ToURI,
		sess.SIPCallID, sess.FinalState, sess.EndReason,
		sess.RTPAudioPort, sess.RTPVideoPort, sess.RTCPAudioPort, sess.RTCPVideoPort,
		sess.SIPOpusPT, sess.AudioProfile, sess.VideoProfile, sess.VideoRejected,
		sess.AuthMode, sess.TrunkID, sess.TrunkName, sess.SIPUsername,
		metaJSON,
	)
	if err == nil {
		return nil
	}

	// Backward compatibility: if DB has not been migrated with auth_* columns yet,
	// fall back to legacy upsert so session tracking keeps working.
	if !isUndefinedColumnError(err) {
		return err
	}

	legacyQuery := `
		INSERT INTO call_sessions (
			session_id, created_at, updated_at, ended_at,
			direction, from_uri, to_uri,
			sip_call_id, final_state, end_reason,
			rtp_audio_port, rtp_video_port, rtcp_audio_port, rtcp_video_port,
			sip_opus_pt, audio_profile, video_profile, video_rejected,
			meta
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
		)
		ON CONFLICT (session_id) DO UPDATE SET
			updated_at = EXCLUDED.updated_at,
			ended_at = EXCLUDED.ended_at,
			direction = COALESCE(NULLIF(EXCLUDED.direction, ''), call_sessions.direction),
			from_uri = COALESCE(NULLIF(EXCLUDED.from_uri, ''), call_sessions.from_uri),
			to_uri = COALESCE(NULLIF(EXCLUDED.to_uri, ''), call_sessions.to_uri),
			sip_call_id = COALESCE(NULLIF(EXCLUDED.sip_call_id, ''), call_sessions.sip_call_id),
			final_state = EXCLUDED.final_state,
			end_reason = EXCLUDED.end_reason,
			rtp_audio_port = EXCLUDED.rtp_audio_port,
			rtp_video_port = EXCLUDED.rtp_video_port,
			rtcp_audio_port = EXCLUDED.rtcp_audio_port,
			rtcp_video_port = EXCLUDED.rtcp_video_port,
			sip_opus_pt = EXCLUDED.sip_opus_pt,
			audio_profile = EXCLUDED.audio_profile,
			video_profile = EXCLUDED.video_profile,
			video_rejected = EXCLUDED.video_rejected,
			meta = EXCLUDED.meta
	`

	_, legacyErr := s.pool.Exec(ctx, legacyQuery,
		sess.SessionID, sess.CreatedAt, sess.UpdatedAt, sess.EndedAt,
		sess.Direction, sess.FromURI, sess.ToURI,
		sess.SIPCallID, sess.FinalState, sess.EndReason,
		sess.RTPAudioPort, sess.RTPVideoPort, sess.RTCPAudioPort, sess.RTCPVideoPort,
		sess.SIPOpusPT, sess.AudioProfile, sess.VideoProfile, sess.VideoRejected,
		metaJSON,
	)
	if legacyErr != nil {
		return fmt.Errorf("upsert session failed on new+legacy schema: new=%v legacy=%w", err, legacyErr)
	}

	return nil
}

func isUndefinedColumnError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42703"
	}
	return false
}

// LogEvent queues an event for async batch insert
func (s *logStore) LogEvent(event *Event) {
	if s.pool == nil {
		return
	}

	select {
	case s.eventQueue <- event:
		// Event queued successfully
	default:
		// Queue full - drop event and log warning
		fmt.Printf("⚠️ Event queue full, dropping event: %s/%s\n", event.Category, event.Name)
	}
}

// StorePayload stores a payload and returns its ID
func (s *logStore) StorePayload(ctx context.Context, payload *PayloadRecord) (int64, error) {
	if s.pool == nil {
		return 0, fmt.Errorf("database pool not initialized")
	}

	parsedJSON, err := json.Marshal(payload.Parsed)
	if err != nil {
		parsedJSON = []byte("{}")
	}

	query := `
		INSERT INTO call_payloads (
			ts, session_id, kind, content_type, body_text, body_bytes_b64, parsed
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		) RETURNING payload_id
	`

	var payloadID int64
	err = s.pool.QueryRow(ctx, query,
		payload.Timestamp, payload.SessionID, payload.Kind,
		payload.ContentType, payload.BodyText, payload.BodyBytesB64,
		parsedJSON,
	).Scan(&payloadID)

	return payloadID, err
}

// RecordStats queues stats for async batch insert
func (s *logStore) RecordStats(stats *StatsRecord) {
	if s.pool == nil {
		return
	}

	select {
	case s.statsQueue <- stats:
		// Stats queued successfully
	default:
		// Queue full - drop stats and log warning
		fmt.Printf("⚠️ Stats queue full, dropping stats for session: %s\n", stats.SessionID)
	}
}

// UpsertDialog inserts or updates a SIP dialog record
func (s *logStore) UpsertDialog(ctx context.Context, dialog *DialogRecord) error {
	if s.pool == nil {
		return fmt.Errorf("database pool not initialized")
	}

	routeSetJSON, err := json.Marshal(dialog.RouteSet)
	if err != nil {
		routeSetJSON = []byte("[]")
	}

	query := `
		INSERT INTO sip_dialogs (
			session_id, ts, sip_call_id, from_tag, to_tag, remote_contact, cseq, route_set
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
	`

	_, err = s.pool.Exec(ctx, query,
		dialog.SessionID, dialog.Timestamp, dialog.SIPCallID,
		dialog.FromTag, dialog.ToTag, dialog.RemoteContact,
		dialog.CSeq, routeSetJSON,
	)

	return err
}

// UpsertSessionDirectory inserts or updates session directory entry
func (s *logStore) UpsertSessionDirectory(ctx context.Context, sessionID, ownerInstanceID, wsURL string, ttlSeconds int) error {
	if s.pool == nil {
		return fmt.Errorf("database pool not initialized")
	}

	expiresAt := time.Now().Add(time.Duration(ttlSeconds) * time.Second)

	query := `
		INSERT INTO session_directory (session_id, owner_instance_id, ws_url, expires_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (session_id)
		DO UPDATE SET
			owner_instance_id = EXCLUDED.owner_instance_id,
			ws_url = EXCLUDED.ws_url,
			expires_at = EXCLUDED.expires_at,
			updated_at = NOW()
	`

	_, err := s.pool.Exec(ctx, query, sessionID, ownerInstanceID, wsURL, expiresAt)
	return err
}

// LookupSessionDirectory looks up session directory entry
func (s *logStore) LookupSessionDirectory(ctx context.Context, sessionID string) (ownerInstanceID, wsURL string, found bool, err error) {
	if s.pool == nil {
		return "", "", false, fmt.Errorf("database pool not initialized")
	}

	query := `
		SELECT owner_instance_id, ws_url
		FROM session_directory
		WHERE session_id = $1 AND expires_at > NOW()
	`

	err = s.pool.QueryRow(ctx, query, sessionID).Scan(&ownerInstanceID, &wsURL)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return "", "", false, nil
		}
		return "", "", false, err
	}

	return ownerInstanceID, wsURL, true, nil
}

// DeleteSessionDirectory deletes a session directory entry
func (s *logStore) DeleteSessionDirectory(ctx context.Context, sessionID string) error {
	if s.pool == nil {
		return fmt.Errorf("database pool not initialized")
	}

	query := `DELETE FROM session_directory WHERE session_id = $1`
	_, err := s.pool.Exec(ctx, query, sessionID)
	return err
}

// sessionDirectoryCleanupWorker periodically cleans up expired session directory entries
func (s *logStore) sessionDirectoryCleanupWorker() {
	defer s.wg.Done()

	// Cleanup every 5 minutes (hardcoded, can be made configurable later)
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		query := `DELETE FROM session_directory WHERE expires_at < NOW()`
		result, err := s.pool.Exec(ctx, query)
		if err != nil {
			fmt.Printf("⚠️ Session directory cleanup failed: %v\n", err)
			return
		}

		if result.RowsAffected() > 0 {
			fmt.Printf("🧹 Cleaned up %d expired session directory entries\n", result.RowsAffected())
		}
	}

	// Initial cleanup on startup
	cleanup()

	for {
		select {
		case <-ticker.C:
			cleanup()

		case <-s.stopCh:
			fmt.Printf("📊 Session directory cleanup worker stopped\n")
			return
		}
	}
}

// UpsertGatewayInstance inserts or updates a gateway instance registry entry
func (s *logStore) UpsertGatewayInstance(ctx context.Context, instanceID, wsURL string, ttlSeconds int) error {
	if s.pool == nil {
		return fmt.Errorf("database pool not initialized")
	}

	expiresAt := time.Now().Add(time.Duration(ttlSeconds) * time.Second)

	query := `
		INSERT INTO gateway_instances (instance_id, ws_url, expires_at, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (instance_id)
		DO UPDATE SET
			ws_url = EXCLUDED.ws_url,
			expires_at = EXCLUDED.expires_at,
			updated_at = NOW()
	`

	_, err := s.pool.Exec(ctx, query, instanceID, wsURL, expiresAt)
	return err
}

// LookupGatewayInstance looks up a gateway instance registry entry
func (s *logStore) LookupGatewayInstance(ctx context.Context, instanceID string) (wsURL string, found bool, err error) {
	if s.pool == nil {
		return "", false, fmt.Errorf("database pool not initialized")
	}

	query := `
		SELECT ws_url
		FROM gateway_instances
		WHERE instance_id = $1 AND expires_at > NOW()
	`

	err = s.pool.QueryRow(ctx, query, instanceID).Scan(&wsURL)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return "", false, nil
		}
		return "", false, err
	}

	return wsURL, true, nil
}

// ResolveTrunkByCredentials resolves trunk by credentials without creating a new trunk.
func (s *logStore) ResolveTrunkByCredentials(ctx context.Context, domain string, port int, username, password string) (trunkID int64, leaseOwner *string, leaseUntil *time.Time, found bool, err error) {
	if s.pool == nil {
		return 0, nil, nil, false, fmt.Errorf("database pool not initialized")
	}

	query := `
		SELECT id, lease_owner, lease_until
		FROM sip_trunks
		WHERE enabled = true AND domain = $1 AND port = $2 AND username = $3 AND password = $4
	`

	err = s.pool.QueryRow(ctx, query, domain, port, username, password).Scan(&trunkID, &leaseOwner, &leaseUntil)
	if err == nil {
		return trunkID, leaseOwner, leaseUntil, true, nil
	}
	if err.Error() == "no rows in result set" {
		return 0, nil, nil, false, nil
	}
	return 0, nil, nil, false, err
}

// ResolveOrCreateTrunk resolves trunk by credentials or creates a new one
func (s *logStore) ResolveOrCreateTrunk(ctx context.Context, domain string, port int, username, password, transport, instanceID string, leaseTTLSeconds int) (trunkID int64, leaseOwner *string, leaseUntil *time.Time, created bool, err error) {
	if s.pool == nil {
		return 0, nil, nil, false, fmt.Errorf("database pool not initialized")
	}

	query := `
		SELECT id, lease_owner, lease_until
		FROM sip_trunks
		WHERE enabled = true AND domain = $1 AND port = $2 AND username = $3 AND password = $4
	`

	err = s.pool.QueryRow(ctx, query, domain, port, username, password).Scan(&trunkID, &leaseOwner, &leaseUntil)
	if err == nil {
		return trunkID, leaseOwner, leaseUntil, false, nil
	}
	if err.Error() != "no rows in result set" {
		return 0, nil, nil, false, err
	}

	if port == 0 {
		port = 5060
	}
	if transport == "" {
		transport = "tcp"
	}
	lease := time.Now().Add(time.Duration(leaseTTLSeconds) * time.Second)
	name := fmt.Sprintf("auto-%s", username)
	publicID := uuid.NewString()

	insert := `
		INSERT INTO sip_trunks (public_id, name, domain, port, username, password, transport, enabled, is_default, lease_owner, lease_until)
		VALUES ($1, $2, $3, $4, $5, $6, $7, true, false, $8, $9)
		RETURNING id, lease_owner, lease_until
	`

	err = s.pool.QueryRow(ctx, insert, publicID, name, domain, port, username, password, transport, instanceID, lease).Scan(&trunkID, &leaseOwner, &leaseUntil)
	if err != nil {
		return 0, nil, nil, false, err
	}

	return trunkID, leaseOwner, leaseUntil, true, nil
}

// gatewayInstanceCleanupWorker periodically cleans up expired gateway instance entries
func (s *logStore) gatewayInstanceCleanupWorker() {
	defer s.wg.Done()

	// Cleanup every 1 minute
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		query := `DELETE FROM gateway_instances WHERE expires_at < NOW()`
		result, err := s.pool.Exec(ctx, query)
		if err != nil {
			fmt.Printf("⚠️ Gateway instance cleanup failed: %v\n", err)
			return
		}

		if result.RowsAffected() > 0 {
			fmt.Printf("🧹 Cleaned up %d expired gateway instances\n", result.RowsAffected())
		}
	}

	cleanup()

	for {
		select {
		case <-ticker.C:
			cleanup()
		case <-s.stopCh:
			fmt.Printf("📊 Gateway instance cleanup worker stopped\n")
			return
		}
	}
}

// ListSessions returns call sessions from the database with pagination, search, and filtering.
func (s *logStore) ListSessions(ctx context.Context, params SessionListParams) (*SessionListResult, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool not initialized")
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

	if params.SessionID != "" {
		where += fmt.Sprintf(" AND cs.session_id = $%d", argIdx)
		args = append(args, params.SessionID)
		argIdx++
	}
	if params.Direction != "" {
		where += fmt.Sprintf(" AND cs.direction = $%d", argIdx)
		args = append(args, params.Direction)
		argIdx++
	}
	if params.State != "" {
		where += fmt.Sprintf(" AND cs.final_state = $%d", argIdx)
		args = append(args, params.State)
		argIdx++
	}
	if params.Search != "" {
		where += fmt.Sprintf(" AND (cs.session_id ILIKE $%d OR COALESCE(NULLIF(cs.from_uri, ''), st.username, '') ILIKE $%d OR cs.to_uri ILIKE $%d OR cs.sip_call_id ILIKE $%d)", argIdx, argIdx+1, argIdx+2, argIdx+3)
		like := "%" + params.Search + "%"
		args = append(args, like, like, like, like)
		argIdx += 4
	}
	if params.CreatedAfter != nil {
		where += fmt.Sprintf(" AND cs.created_at >= $%d", argIdx)
		args = append(args, *params.CreatedAfter)
		argIdx++
	}
	if params.CreatedBefore != nil {
		where += fmt.Sprintf(" AND cs.created_at <= $%d", argIdx)
		args = append(args, *params.CreatedBefore)
		argIdx++
	}

	// Count total matching rows
	countSQL := `
		SELECT COUNT(*)
		FROM call_sessions cs
		LEFT JOIN sip_trunks st
		  ON (cs.meta->>'trunkId') ~ '^[0-9]+$'
		 AND (cs.meta->>'trunkId')::bigint = st.id
	` + where
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count query failed: %w", err)
	}

	// Fetch page
	offset := (params.Page - 1) * params.PageSize
	dataSQL := fmt.Sprintf(`
		SELECT cs.session_id, cs.created_at, cs.updated_at, cs.ended_at,
		       cs.direction, COALESCE(NULLIF(cs.from_uri, ''), st.username, '') AS from_uri, cs.to_uri,
		       cs.sip_call_id, cs.final_state, cs.end_reason,
		       cs.rtp_audio_port, cs.rtp_video_port, cs.rtcp_audio_port, cs.rtcp_video_port,
		       cs.sip_opus_pt, COALESCE(cs.audio_profile,''), COALESCE(cs.video_profile,''), COALESCE(cs.video_rejected, false),
		       cs.meta
		FROM call_sessions cs
		LEFT JOIN sip_trunks st
		  ON (cs.meta->>'trunkId') ~ '^[0-9]+$'
		 AND (cs.meta->>'trunkId')::bigint = st.id
		%s
		ORDER BY cs.created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1)
	args = append(args, params.PageSize, offset)

	rows, err := s.pool.Query(ctx, dataSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	sessions := make([]*SessionRecord, 0)
	for rows.Next() {
		sess := &SessionRecord{Meta: make(map[string]interface{})}
		var metaJSON []byte
		err := rows.Scan(
			&sess.SessionID, &sess.CreatedAt, &sess.UpdatedAt, &sess.EndedAt,
			&sess.Direction, &sess.FromURI, &sess.ToURI,
			&sess.SIPCallID, &sess.FinalState, &sess.EndReason,
			&sess.RTPAudioPort, &sess.RTPVideoPort, &sess.RTCPAudioPort, &sess.RTCPVideoPort,
			&sess.SIPOpusPT, &sess.AudioProfile, &sess.VideoProfile, &sess.VideoRejected,
			&metaJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		if len(metaJSON) > 0 {
			_ = json.Unmarshal(metaJSON, &sess.Meta)
		}
		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	return &SessionListResult{
		Items:    sessions,
		Total:    total,
		Page:     params.Page,
		PageSize: params.PageSize,
	}, nil
}

// normalisePagination normalises pagination defaults
func normalisePagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

// ListEvents returns call events for a session with pagination and filtering.
func (s *logStore) ListEvents(ctx context.Context, params EventListParams) (*EventListResult, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool not initialized")
	}

	params.Page, params.PageSize = normalisePagination(params.Page, params.PageSize)

	where := "WHERE session_id = $1"
	args := []interface{}{params.SessionID}
	argIdx := 2

	if params.Category != "" {
		where += fmt.Sprintf(" AND category = $%d", argIdx)
		args = append(args, params.Category)
		argIdx++
	}
	if params.Name != "" {
		where += fmt.Sprintf(" AND name ILIKE $%d", argIdx)
		args = append(args, "%"+params.Name+"%")
		argIdx++
	}

	countSQL := "SELECT COUNT(*) FROM call_events " + where
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count query failed: %w", err)
	}

	offset := (params.Page - 1) * params.PageSize
	dataSQL := fmt.Sprintf(`
		SELECT id, ts, session_id, category, name,
		       COALESCE(sip_method,''), COALESCE(sip_status_code,0), COALESCE(sip_call_id,''),
		       COALESCE(state,''), payload_id, data
		FROM call_events
		%s
		ORDER BY ts ASC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1)
	args = append(args, params.PageSize, offset)

	rows, err := s.pool.Query(ctx, dataSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	items := make([]*EventRecord, 0)
	for rows.Next() {
		ev := &EventRecord{Data: make(map[string]interface{})}
		var dataJSON []byte
		err := rows.Scan(
			&ev.ID, &ev.Timestamp, &ev.SessionID, &ev.Category, &ev.Name,
			&ev.SIPMethod, &ev.SIPStatusCode, &ev.SIPCallID,
			&ev.State, &ev.PayloadID, &dataJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		if len(dataJSON) > 0 {
			_ = json.Unmarshal(dataJSON, &ev.Data)
		}
		items = append(items, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	return &EventListResult{Items: items, Total: total, Page: params.Page, PageSize: params.PageSize}, nil
}

// ListPayloads returns call payloads for a session with pagination and filtering.
func (s *logStore) ListPayloads(ctx context.Context, params PayloadListParams) (*PayloadListResult, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool not initialized")
	}

	params.Page, params.PageSize = normalisePagination(params.Page, params.PageSize)

	where := "WHERE session_id = $1"
	args := []interface{}{params.SessionID}
	argIdx := 2

	if params.Kind != "" {
		where += fmt.Sprintf(" AND kind = $%d", argIdx)
		args = append(args, params.Kind)
		argIdx++
	}

	countSQL := "SELECT COUNT(*) FROM call_payloads " + where
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count query failed: %w", err)
	}

	offset := (params.Page - 1) * params.PageSize
	dataSQL := fmt.Sprintf(`
		SELECT payload_id, ts, session_id, kind, COALESCE(content_type,''),
		       COALESCE(body_text,''), COALESCE(body_bytes_b64,''), parsed
		FROM call_payloads
		%s
		ORDER BY ts ASC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1)
	args = append(args, params.PageSize, offset)

	rows, err := s.pool.Query(ctx, dataSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	items := make([]*PayloadReadRecord, 0)
	for rows.Next() {
		p := &PayloadReadRecord{Parsed: make(map[string]interface{})}
		var parsedJSON []byte
		err := rows.Scan(
			&p.PayloadID, &p.Timestamp, &p.SessionID, &p.Kind, &p.ContentType,
			&p.BodyText, &p.BodyBytesB64, &parsedJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		if len(parsedJSON) > 0 {
			_ = json.Unmarshal(parsedJSON, &p.Parsed)
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	return &PayloadListResult{Items: items, Total: total, Page: params.Page, PageSize: params.PageSize}, nil
}

// GetPayload returns a single payload by ID.
func (s *logStore) GetPayload(ctx context.Context, payloadID int64) (*PayloadReadRecord, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool not initialized")
	}

	query := `
		SELECT payload_id, ts, session_id, kind, COALESCE(content_type,''),
		       COALESCE(body_text,''), COALESCE(body_bytes_b64,''), parsed
		FROM call_payloads
		WHERE payload_id = $1
	`

	p := &PayloadReadRecord{Parsed: make(map[string]interface{})}
	var parsedJSON []byte
	err := s.pool.QueryRow(ctx, query, payloadID).Scan(
		&p.PayloadID, &p.Timestamp, &p.SessionID, &p.Kind, &p.ContentType,
		&p.BodyText, &p.BodyBytesB64, &parsedJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("payload not found: %w", err)
	}
	if len(parsedJSON) > 0 {
		_ = json.Unmarshal(parsedJSON, &p.Parsed)
	}

	return p, nil
}

// ListDialogs returns SIP dialog snapshots for a session with pagination.
func (s *logStore) ListDialogs(ctx context.Context, params DialogListParams) (*DialogListResult, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool not initialized")
	}

	params.Page, params.PageSize = normalisePagination(params.Page, params.PageSize)

	where := "WHERE session_id = $1"
	args := []interface{}{params.SessionID}
	argIdx := 2

	countSQL := "SELECT COUNT(*) FROM sip_dialogs " + where
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count query failed: %w", err)
	}

	offset := (params.Page - 1) * params.PageSize
	dataSQL := fmt.Sprintf(`
		SELECT id, session_id, ts, COALESCE(sip_call_id,''),
		       COALESCE(from_tag,''), COALESCE(to_tag,''), COALESCE(remote_contact,''),
		       COALESCE(cseq,0), route_set
		FROM sip_dialogs
		%s
		ORDER BY ts ASC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1)
	args = append(args, params.PageSize, offset)

	rows, err := s.pool.Query(ctx, dataSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	items := make([]*DialogRecord, 0)
	for rows.Next() {
		d := &DialogRecord{}
		var routeSetJSON []byte
		err := rows.Scan(
			&d.ID, &d.SessionID, &d.Timestamp, &d.SIPCallID,
			&d.FromTag, &d.ToTag, &d.RemoteContact,
			&d.CSeq, &routeSetJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		if len(routeSetJSON) > 0 {
			_ = json.Unmarshal(routeSetJSON, &d.RouteSet)
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	return &DialogListResult{Items: items, Total: total, Page: params.Page, PageSize: params.PageSize}, nil
}

// ListStats returns call stats for a session with pagination.
func (s *logStore) ListStats(ctx context.Context, params StatsListParams) (*StatsListResult, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool not initialized")
	}

	params.Page, params.PageSize = normalisePagination(params.Page, params.PageSize)

	where := "WHERE session_id = $1"
	args := []interface{}{params.SessionID}
	argIdx := 2

	countSQL := "SELECT COUNT(*) FROM call_stats " + where
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count query failed: %w", err)
	}

	offset := (params.Page - 1) * params.PageSize
	dataSQL := fmt.Sprintf(`
		SELECT id, ts, session_id,
		       COALESCE(pli_sent,0), COALESCE(pli_response,0),
		       last_pli_sent_at, last_keyframe_at,
		       COALESCE(audio_rtcp_rr,0), COALESCE(audio_rtcp_sr,0),
		       COALESCE(video_rtcp_rr,0), COALESCE(video_rtcp_sr,0),
		       data
		FROM call_stats
		%s
		ORDER BY ts ASC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1)
	args = append(args, params.PageSize, offset)

	rows, err := s.pool.Query(ctx, dataSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	items := make([]*StatsReadRecord, 0)
	for rows.Next() {
		st := &StatsReadRecord{Data: make(map[string]interface{})}
		var dataJSON []byte
		err := rows.Scan(
			&st.ID, &st.Timestamp, &st.SessionID,
			&st.PLISent, &st.PLIResponse,
			&st.LastPLISentAt, &st.LastKeyframeAt,
			&st.AudioRTCPRR, &st.AudioRTCPSR,
			&st.VideoRTCPRR, &st.VideoRTCPSR,
			&dataJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		if len(dataJSON) > 0 {
			_ = json.Unmarshal(dataJSON, &st.Data)
		}
		items = append(items, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	return &StatsListResult{Items: items, Total: total, Page: params.Page, PageSize: params.PageSize}, nil
}

// ListGatewayInstances returns gateway instances with pagination.
func (s *logStore) ListGatewayInstances(ctx context.Context, params GatewayInstanceListParams) (*GatewayInstanceListResult, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool not initialized")
	}

	params.Page, params.PageSize = normalisePagination(params.Page, params.PageSize)

	where := "WHERE 1=1"
	args := []interface{}{}
	argIdx := 1

	if params.Search != "" {
		where += fmt.Sprintf(" AND (instance_id ILIKE $%d OR ws_url ILIKE $%d)", argIdx, argIdx+1)
		like := "%" + params.Search + "%"
		args = append(args, like, like)
		argIdx += 2
	}

	countSQL := "SELECT COUNT(*) FROM gateway_instances " + where
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count query failed: %w", err)
	}

	offset := (params.Page - 1) * params.PageSize
	dataSQL := fmt.Sprintf(`
		SELECT instance_id, ws_url, expires_at, updated_at
		FROM gateway_instances
		%s
		ORDER BY updated_at DESC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1)
	args = append(args, params.PageSize, offset)

	rows, err := s.pool.Query(ctx, dataSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	items := make([]*GatewayInstanceRecord, 0)
	for rows.Next() {
		gi := &GatewayInstanceRecord{}
		err := rows.Scan(&gi.InstanceID, &gi.WSURL, &gi.ExpiresAt, &gi.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		items = append(items, gi)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	return &GatewayInstanceListResult{Items: items, Total: total, Page: params.Page, PageSize: params.PageSize}, nil
}

// ListSessionDirectory returns session directory entries with pagination.
func (s *logStore) ListSessionDirectory(ctx context.Context, params SessionDirectoryListParams) (*SessionDirectoryListResult, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool not initialized")
	}

	params.Page, params.PageSize = normalisePagination(params.Page, params.PageSize)

	where := "WHERE 1=1"
	args := []interface{}{}
	argIdx := 1

	if params.Search != "" {
		where += fmt.Sprintf(" AND (session_id ILIKE $%d OR owner_instance_id ILIKE $%d OR ws_url ILIKE $%d)", argIdx, argIdx+1, argIdx+2)
		like := "%" + params.Search + "%"
		args = append(args, like, like, like)
		argIdx += 3
	}

	countSQL := "SELECT COUNT(*) FROM session_directory " + where
	var total int
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count query failed: %w", err)
	}

	offset := (params.Page - 1) * params.PageSize
	dataSQL := fmt.Sprintf(`
		SELECT session_id, owner_instance_id, ws_url, expires_at, updated_at
		FROM session_directory
		%s
		ORDER BY updated_at DESC
		LIMIT $%d OFFSET $%d
	`, where, argIdx, argIdx+1)
	args = append(args, params.PageSize, offset)

	rows, err := s.pool.Query(ctx, dataSQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	items := make([]*SessionDirectoryRecord, 0)
	for rows.Next() {
		sd := &SessionDirectoryRecord{}
		err := rows.Scan(&sd.SessionID, &sd.OwnerInstanceID, &sd.WSURL, &sd.ExpiresAt, &sd.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		items = append(items, sd)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	return &SessionDirectoryListResult{Items: items, Total: total, Page: params.Page, PageSize: params.PageSize}, nil
}

// GetDashboardSummary returns aggregate metrics for dashboard cards and charts.
func (s *logStore) GetDashboardSummary(ctx context.Context, params DashboardSummaryParams) (*DashboardSummaryResult, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool not initialized")
	}

	period := params.Period
	switch period {
	case "day", "month", "year":
	default:
		return nil, fmt.Errorf("invalid period: %s", period)
	}

	timezone := params.Timezone
	if timezone == "" {
		timezone = "Asia/Bangkok"
	}

	topTrunks := params.TopTrunks
	if topTrunks < 1 {
		topTrunks = 10
	}

	result := &DashboardSummaryResult{
		Series:     make([]DashboardSeriesPoint, 0),
		States:     make([]DashboardStateCount, 0),
		Directions: make([]DashboardDirectionCount, 0),
		TopTrunks:  make([]DashboardTrunkCount, 0),
	}

	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM call_sessions WHERE created_at >= $1 AND created_at < $2`,
		params.RangeStartUTC,
		params.RangeEndUTC,
	).Scan(&result.TotalSessions); err != nil {
		return nil, fmt.Errorf("dashboard total sessions query failed: %w", err)
	}

	// Duration stats: average and max call duration (seconds)
	if err := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(AVG(EXTRACT(EPOCH FROM (ended_at - created_at)))::double precision, 0),
			COALESCE(MAX(EXTRACT(EPOCH FROM (ended_at - created_at)))::int, 0)
		FROM call_sessions
		WHERE created_at >= $1 AND created_at < $2
		  AND ended_at IS NOT NULL
	`, params.RangeStartUTC, params.RangeEndUTC).Scan(&result.AvgDurationSec, &result.MaxDurationSec); err != nil {
		return nil, fmt.Errorf("dashboard duration query failed: %w", err)
	}

	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM session_directory WHERE expires_at >= NOW()`,
	).Scan(&result.SessionDirectoryCount); err != nil {
		return nil, fmt.Errorf("dashboard session directory count query failed: %w", err)
	}

	bucketExpr := "date_trunc('hour', created_at AT TIME ZONE $1)"
	bucketFormat := "YYYY-MM-DD HH24:00"
	if period == "month" {
		bucketExpr = "date_trunc('day', created_at AT TIME ZONE $1)"
		bucketFormat = "YYYY-MM-DD"
	} else if period == "year" {
		bucketExpr = "date_trunc('month', created_at AT TIME ZONE $1)"
		bucketFormat = "YYYY-MM"
	}

	seriesSQL := fmt.Sprintf(`
		SELECT to_char(%s, '%s') AS bucket, COUNT(*)
		FROM call_sessions
		WHERE created_at >= $2 AND created_at < $3
		GROUP BY 1
		ORDER BY 1 ASC
	`, bucketExpr, bucketFormat)

	seriesRows, err := s.pool.Query(ctx, seriesSQL, timezone, params.RangeStartUTC, params.RangeEndUTC)
	if err != nil {
		return nil, fmt.Errorf("dashboard series query failed: %w", err)
	}
	defer seriesRows.Close()

	for seriesRows.Next() {
		var item DashboardSeriesPoint
		if err := seriesRows.Scan(&item.Bucket, &item.Count); err != nil {
			return nil, fmt.Errorf("dashboard series scan failed: %w", err)
		}
		result.Series = append(result.Series, item)
	}
	if err := seriesRows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard series rows failed: %w", err)
	}

	stateRows, err := s.pool.Query(ctx, `
		SELECT COALESCE(NULLIF(final_state, ''), 'unknown') AS state, COUNT(*)
		FROM call_sessions
		WHERE created_at >= $1 AND created_at < $2
		GROUP BY 1
		ORDER BY COUNT(*) DESC, state ASC
	`, params.RangeStartUTC, params.RangeEndUTC)
	if err != nil {
		return nil, fmt.Errorf("dashboard state query failed: %w", err)
	}
	defer stateRows.Close()

	for stateRows.Next() {
		var item DashboardStateCount
		if err := stateRows.Scan(&item.State, &item.Count); err != nil {
			return nil, fmt.Errorf("dashboard state scan failed: %w", err)
		}
		result.States = append(result.States, item)
	}
	if err := stateRows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard state rows failed: %w", err)
	}

	// Direction breakdown (inbound / outbound)
	dirRows, err := s.pool.Query(ctx, `
		SELECT direction, COUNT(*)
		FROM call_sessions
		WHERE created_at >= $1 AND created_at < $2
		GROUP BY direction
		ORDER BY COUNT(*) DESC
	`, params.RangeStartUTC, params.RangeEndUTC)
	if err != nil {
		return nil, fmt.Errorf("dashboard direction query failed: %w", err)
	}
	defer dirRows.Close()

	for dirRows.Next() {
		var item DashboardDirectionCount
		if err := dirRows.Scan(&item.Direction, &item.Count); err != nil {
			return nil, fmt.Errorf("dashboard direction scan failed: %w", err)
		}
		result.Directions = append(result.Directions, item)
	}
	if err := dirRows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard direction rows failed: %w", err)
	}

	trunkRows, err := s.pool.Query(ctx, `
		SELECT
			COALESCE(trunk_id::text, 'unknown') AS trunk_key,
			COALESCE(NULLIF(trunk_name, ''), CASE WHEN trunk_id IS NOT NULL THEN CONCAT('Trunk #', trunk_id::text) ELSE 'Public/Unknown' END) AS trunk_name,
			COUNT(*)
		FROM call_sessions
		WHERE created_at >= $1 AND created_at < $2
		GROUP BY 1, 2
		ORDER BY COUNT(*) DESC, trunk_name ASC
		LIMIT $3
	`, params.RangeStartUTC, params.RangeEndUTC, topTrunks)
	if err != nil {
		return nil, fmt.Errorf("dashboard trunk query failed: %w", err)
	}
	defer trunkRows.Close()

	for trunkRows.Next() {
		var item DashboardTrunkCount
		if err := trunkRows.Scan(&item.TrunkKey, &item.TrunkName, &item.Count); err != nil {
			return nil, fmt.Errorf("dashboard trunk scan failed: %w", err)
		}
		result.TopTrunks = append(result.TopTrunks, item)
	}
	if err := trunkRows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard trunk rows failed: %w", err)
	}

	return result, nil
}

// GetDB returns the underlying database pool (for TrunkManager)
// Returns *pgxpool.Pool wrapped as interface{}
func (s *logStore) GetDB() *pgxpool.Pool {
	return s.pool
}

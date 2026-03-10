package logstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"k2-gateway/internal/config"
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
	ResolveOrCreateTrunk(ctx context.Context, domain string, port int, username, password, transport, instanceID string, leaseTTLSeconds int) (trunkID int64, leaseOwner string, leaseUntil *time.Time, created bool, err error)

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
func (n *noopStore) ResolveOrCreateTrunk(ctx context.Context, domain string, port int, username, password, transport, instanceID string, leaseTTLSeconds int) (int64, string, *time.Time, bool, error) {
	return 0, "", nil, false, nil
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
			meta
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
		)
		ON CONFLICT (session_id) DO UPDATE SET
			updated_at = EXCLUDED.updated_at,
			ended_at = EXCLUDED.ended_at,
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

	_, err = s.pool.Exec(ctx, query,
		sess.SessionID, sess.CreatedAt, sess.UpdatedAt, sess.EndedAt,
		sess.Direction, sess.FromURI, sess.ToURI,
		sess.SIPCallID, sess.FinalState, sess.EndReason,
		sess.RTPAudioPort, sess.RTPVideoPort, sess.RTCPAudioPort, sess.RTCPVideoPort,
		sess.SIPOpusPT, sess.AudioProfile, sess.VideoProfile, sess.VideoRejected,
		metaJSON,
	)

	return err
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

// ResolveOrCreateTrunk resolves trunk by credentials or creates a new one
func (s *logStore) ResolveOrCreateTrunk(ctx context.Context, domain string, port int, username, password, transport, instanceID string, leaseTTLSeconds int) (trunkID int64, leaseOwner string, leaseUntil *time.Time, created bool, err error) {
	if s.pool == nil {
		return 0, "", nil, false, fmt.Errorf("database pool not initialized")
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
		return 0, "", nil, false, err
	}

	if port == 0 {
		port = 5060
	}
	if transport == "" {
		transport = "tcp"
	}
	lease := time.Now().Add(time.Duration(leaseTTLSeconds) * time.Second)
	name := fmt.Sprintf("auto-%s@%s:%d-%d", username, domain, port, time.Now().UnixNano())

	insert := `
		INSERT INTO sip_trunks (name, domain, port, username, password, transport, enabled, is_default, lease_owner, lease_until)
		VALUES ($1, $2, $3, $4, $5, $6, true, false, $7, $8)
		RETURNING id, lease_owner, lease_until
	`

	err = s.pool.QueryRow(ctx, insert, name, domain, port, username, password, transport, instanceID, lease).Scan(&trunkID, &leaseOwner, &leaseUntil)
	if err != nil {
		return 0, "", nil, false, err
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

// GetDB returns the underlying database pool (for TrunkManager)
// Returns *pgxpool.Pool wrapped as interface{}
func (s *logStore) GetDB() *pgxpool.Pool {
	return s.pool
}

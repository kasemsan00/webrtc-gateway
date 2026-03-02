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
	s.wg.Add(3)
	go s.eventBatchWorker()
	go s.statsBatchWorker()
	go s.partitionMaintenanceWorker()

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

package logstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"webrtc-sip-gateway/internal/telemetry"
)

var (
	callEventsColumns = []string{
		"ts", "session_id", "category", "name",
		"sip_method", "sip_status_code", "sip_call_id", "state",
		"payload_id", "data",
	}
	callStatsColumns = []string{
		"ts", "session_id",
		"pli_sent", "pli_response", "last_pli_sent_at", "last_keyframe_at",
		"audio_rtcp_rr", "audio_rtcp_sr", "video_rtcp_rr", "video_rtcp_sr",
		"data",
	}
)

const (
	callEventsInsertSQL = `
		INSERT INTO call_events (
			ts, session_id, category, name,
			sip_method, sip_status_code, sip_call_id, state,
			payload_id, data
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		)
	`
	callStatsInsertSQL = `
		INSERT INTO call_stats (
			ts, session_id,
			pli_sent, pli_response, last_pli_sent_at, last_keyframe_at,
			audio_rtcp_rr, audio_rtcp_sr, video_rtcp_rr, video_rtcp_sr,
			data
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)
	`
)

func marshalJSONData(data map[string]interface{}) []byte {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return []byte("{}")
	}
	return dataJSON
}

func (s *logStore) copyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	if s.copyFromHook != nil {
		return s.copyFromHook(ctx, tableName, columnNames, rowSrc)
	}
	if s.pool == nil {
		return 0, fmt.Errorf("database pool not initialized")
	}
	return s.pool.CopyFrom(ctx, tableName, columnNames, rowSrc)
}

func (s *logStore) sendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	if s.sendBatchHook != nil {
		return s.sendBatchHook(ctx, b)
	}
	return s.pool.SendBatch(ctx, b)
}

// eventBatchWorker processes events in batches
func (s *logStore) eventBatchWorker() {
	defer s.wg.Done()

	batchSize := s.config.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	batchIntervalMS := s.config.BatchIntervalMS
	if batchIntervalMS <= 0 {
		batchIntervalMS = 1000
	}

	batch := make([]*Event, 0, batchSize)
	ticker := time.NewTicker(time.Duration(batchIntervalMS) * time.Millisecond)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		started := time.Now()
		err := s.execBatchInsertEvents(ctx, batch)
		if err != nil {
			fmt.Printf("❌ Failed to insert event batch (%d events): %v\n", len(batch), err)
		} else {
			s.lastSuccessNS.Store(time.Now().UnixNano())
		}
		recordPersistenceBatchTelemetry("events", started, len(batch), err)

		batch = batch[:0] // Clear batch (reuse underlying array)
	}

	for {
		select {
		case event := <-s.eventQueue:
			batch = append(batch, event)
			if len(batch) >= batchSize {
				flush()
			}

		case <-ticker.C:
			flush()

		case <-s.stopCh:
			// Drain all queued events before shutdown
			deadline := time.Now().Add(defaultShutdownDrainTimeout)
			for {
				select {
				case event := <-s.eventQueue:
					batch = append(batch, event)
					if len(batch) >= batchSize {
						flush()
						if time.Now().After(deadline) {
							fmt.Printf("📊 Event batch worker stopped\n")
							return
						}
					}
				default:
					flush()
					fmt.Printf("📊 Event batch worker stopped\n")
					return
				}
			}
		}
	}
}

func (s *logStore) execBatchInsertEvents(ctx context.Context, events []*Event) error {
	if s.batchEventsHook != nil {
		return s.batchEventsHook(ctx, events)
	}
	return s.batchInsertEvents(ctx, events)
}

// batchInsertEvents inserts multiple events using pgx.CopyFrom with a row-wise
// pgx.Batch fallback if CopyFrom fails.
func (s *logStore) batchInsertEvents(ctx context.Context, events []*Event) error {
	if len(events) == 0 {
		return nil
	}

	rows := make([][]any, 0, len(events))
	for _, event := range events {
		if event == nil {
			continue
		}
		rows = append(rows, []any{
			event.Timestamp, event.SessionID, event.Category, event.Name,
			event.SIPMethod, event.SIPStatusCode, event.SIPCallID, event.State,
			event.PayloadID, marshalJSONData(event.Data),
		})
	}
	if len(rows) == 0 {
		return nil
	}

	if _, err := s.copyFrom(ctx, pgx.Identifier{"call_events"}, callEventsColumns, pgx.CopyFromRows(rows)); err == nil {
		return nil
	} else if ctx.Err() != nil {
		return fmt.Errorf("failed to copy events: %w", err)
	} else {
		fmt.Printf("⚠️ CopyFrom call_events failed (%d events), falling back to row-wise batch: %v\n", len(rows), err)
	}

	return s.batchInsertEventsFallback(ctx, rows)
}

func (s *logStore) batchInsertEventsFallback(ctx context.Context, rows [][]any) error {
	if s.pool == nil && s.sendBatchHook == nil {
		return fmt.Errorf("database pool not initialized")
	}

	batch := &pgx.Batch{}
	for _, row := range rows {
		batch.Queue(callEventsInsertSQL, row...)
	}

	batchResults := s.sendBatch(ctx, batch)
	defer batchResults.Close()

	for i := 0; i < len(rows); i++ {
		if _, err := batchResults.Exec(); err != nil {
			return fmt.Errorf("failed to insert event %d: %w", i, err)
		}
	}

	return nil
}

// statsBatchWorker processes stats in batches
func (s *logStore) statsBatchWorker() {
	defer s.wg.Done()

	batchSize := s.config.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	batchIntervalMS := s.config.BatchIntervalMS
	if batchIntervalMS <= 0 {
		batchIntervalMS = 1000
	}

	batch := make([]*StatsRecord, 0, batchSize)
	ticker := time.NewTicker(time.Duration(batchIntervalMS) * time.Millisecond)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		started := time.Now()
		err := s.execBatchInsertStats(ctx, batch)
		if err != nil {
			fmt.Printf("❌ Failed to insert stats batch (%d records): %v\n", len(batch), err)
		} else {
			s.lastSuccessNS.Store(time.Now().UnixNano())
		}
		recordPersistenceBatchTelemetry("stats", started, len(batch), err)

		batch = batch[:0] // Clear batch
	}

	for {
		select {
		case stats := <-s.statsQueue:
			batch = append(batch, stats)
			if len(batch) >= batchSize {
				flush()
			}

		case <-ticker.C:
			flush()

		case <-s.stopCh:
			// Drain all queued stats before shutdown
			deadline := time.Now().Add(defaultShutdownDrainTimeout)
			for {
				select {
				case stats := <-s.statsQueue:
					batch = append(batch, stats)
					if len(batch) >= batchSize {
						flush()
						if time.Now().After(deadline) {
							fmt.Printf("📊 Stats batch worker stopped\n")
							return
						}
					}
				default:
					flush()
					fmt.Printf("📊 Stats batch worker stopped\n")
					return
				}
			}
		}
	}
}

func (s *logStore) execBatchInsertStats(ctx context.Context, stats []*StatsRecord) error {
	if s.batchStatsHook != nil {
		return s.batchStatsHook(ctx, stats)
	}
	return s.batchInsertStats(ctx, stats)
}

func persistenceTelemetryReason(err error) string {
	if err != nil {
		return "dependency_unavailable"
	}
	return "none"
}

func recordPersistenceBatchTelemetry(component string, started time.Time, count int, err error) {
	outcome := "success"
	if err != nil {
		outcome = "failure"
	}
	duration := time.Since(started)
	_ = telemetry.Log(context.Background(), telemetry.LogEvent{Severity: telemetry.SeverityInfo, Component: "persistence", Name: "persistence.batch.completed", Outcome: outcome, Reason: persistenceTelemetryReason(err), Measurements: []telemetry.Measurement{telemetry.Int64Measurement("count", int64(count)), telemetry.DurationMilliseconds("duration_ms", duration)}})
}

// batchInsertStats inserts multiple stats records using pgx.CopyFrom with a
// row-wise pgx.Batch fallback if CopyFrom fails.
func (s *logStore) batchInsertStats(ctx context.Context, stats []*StatsRecord) error {
	if len(stats) == 0 {
		return nil
	}

	rows := make([][]any, 0, len(stats))
	for _, stat := range stats {
		if stat == nil {
			continue
		}
		rows = append(rows, []any{
			stat.Timestamp, stat.SessionID,
			stat.PLISent, stat.PLIResponse, stat.LastPLISentAt, stat.LastKeyframeAt,
			stat.AudioRTCPRR, stat.AudioRTCPSR, stat.VideoRTCPRR, stat.VideoRTCPSR,
			marshalJSONData(stat.Data),
		})
	}
	if len(rows) == 0 {
		return nil
	}

	if _, err := s.copyFrom(ctx, pgx.Identifier{"call_stats"}, callStatsColumns, pgx.CopyFromRows(rows)); err == nil {
		return nil
	} else if ctx.Err() != nil {
		return fmt.Errorf("failed to copy stats: %w", err)
	} else {
		fmt.Printf("⚠️ CopyFrom call_stats failed (%d records), falling back to row-wise batch: %v\n", len(rows), err)
	}

	return s.batchInsertStatsFallback(ctx, rows)
}

func (s *logStore) batchInsertStatsFallback(ctx context.Context, rows [][]any) error {
	if s.pool == nil && s.sendBatchHook == nil {
		return fmt.Errorf("database pool not initialized")
	}

	batch := &pgx.Batch{}
	for _, row := range rows {
		batch.Queue(callStatsInsertSQL, row...)
	}

	batchResults := s.sendBatch(ctx, batch)
	defer batchResults.Close()

	for i := 0; i < len(rows); i++ {
		if _, err := batchResults.Exec(); err != nil {
			return fmt.Errorf("failed to insert stats %d: %w", i, err)
		}
	}

	return nil
}

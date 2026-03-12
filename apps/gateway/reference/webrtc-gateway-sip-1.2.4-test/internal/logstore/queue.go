package logstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// eventBatchWorker processes events in batches
func (s *logStore) eventBatchWorker() {
	defer s.wg.Done()

	batch := make([]*Event, 0, s.config.BatchSize)
	ticker := time.NewTicker(time.Duration(s.config.BatchIntervalMS) * time.Millisecond)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.batchInsertEvents(ctx, batch); err != nil {
			fmt.Printf("❌ Failed to insert event batch (%d events): %v\n", len(batch), err)
		}

		batch = batch[:0] // Clear batch (reuse underlying array)
	}

	for {
		select {
		case event := <-s.eventQueue:
			batch = append(batch, event)
			if len(batch) >= s.config.BatchSize {
				flush()
			}

		case <-ticker.C:
			flush()

		case <-s.stopCh:
			// Final flush before shutdown
			flush()
			fmt.Printf("📊 Event batch worker stopped\n")
			return
		}
	}
}

// batchInsertEvents inserts multiple events using pgx Batch
func (s *logStore) batchInsertEvents(ctx context.Context, events []*Event) error {
	if len(events) == 0 {
		return nil
	}

	batch := &pgx.Batch{}

	for _, event := range events {
		dataJSON, err := json.Marshal(event.Data)
		if err != nil {
			dataJSON = []byte("{}")
		}

		batch.Queue(`
			INSERT INTO call_events (
				ts, session_id, category, name,
				sip_method, sip_status_code, sip_call_id, state,
				payload_id, data
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
			)
		`,
			event.Timestamp, event.SessionID, event.Category, event.Name,
			event.SIPMethod, event.SIPStatusCode, event.SIPCallID, event.State,
			event.PayloadID, dataJSON,
		)
	}

	// Execute batch
	batchResults := s.pool.SendBatch(ctx, batch)
	defer batchResults.Close()

	// Check for errors (drain all results)
	for i := 0; i < len(events); i++ {
		_, err := batchResults.Exec()
		if err != nil {
			return fmt.Errorf("failed to insert event %d: %w", i, err)
		}
	}

	return nil
}

// statsBatchWorker processes stats in batches
func (s *logStore) statsBatchWorker() {
	defer s.wg.Done()

	batch := make([]*StatsRecord, 0, s.config.BatchSize)
	ticker := time.NewTicker(time.Duration(s.config.BatchIntervalMS) * time.Millisecond)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.batchInsertStats(ctx, batch); err != nil {
			fmt.Printf("❌ Failed to insert stats batch (%d records): %v\n", len(batch), err)
		}

		batch = batch[:0] // Clear batch
	}

	for {
		select {
		case stats := <-s.statsQueue:
			batch = append(batch, stats)
			if len(batch) >= s.config.BatchSize {
				flush()
			}

		case <-ticker.C:
			flush()

		case <-s.stopCh:
			// Final flush before shutdown
			flush()
			fmt.Printf("📊 Stats batch worker stopped\n")
			return
		}
	}
}

// batchInsertStats inserts multiple stats records using pgx Batch
func (s *logStore) batchInsertStats(ctx context.Context, stats []*StatsRecord) error {
	if len(stats) == 0 {
		return nil
	}

	batch := &pgx.Batch{}

	for _, stat := range stats {
		dataJSON, err := json.Marshal(stat.Data)
		if err != nil {
			dataJSON = []byte("{}")
		}

		batch.Queue(`
			INSERT INTO call_stats (
				ts, session_id,
				pli_sent, pli_response, last_pli_sent_at, last_keyframe_at,
				audio_rtcp_rr, audio_rtcp_sr, video_rtcp_rr, video_rtcp_sr,
				data
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
			)
		`,
			stat.Timestamp, stat.SessionID,
			stat.PLISent, stat.PLIResponse, stat.LastPLISentAt, stat.LastKeyframeAt,
			stat.AudioRTCPRR, stat.AudioRTCPSR, stat.VideoRTCPRR, stat.VideoRTCPSR,
			dataJSON,
		)
	}

	// Execute batch
	batchResults := s.pool.SendBatch(ctx, batch)
	defer batchResults.Close()

	// Check for errors (drain all results)
	for i := 0; i < len(stats); i++ {
		_, err := batchResults.Exec()
		if err != nil {
			return fmt.Errorf("failed to insert stats %d: %w", i, err)
		}
	}

	return nil
}

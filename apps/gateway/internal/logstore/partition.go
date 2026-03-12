package logstore

import (
	"context"
	"fmt"
	"time"
)

// partitionMaintenanceWorker creates future partitions and drops old ones
func (s *logStore) partitionMaintenanceWorker() {
	defer s.wg.Done()

	// Run immediately on startup
	s.maintainPartitions()

	// Then run daily
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.maintainPartitions()

		case <-s.stopCh:
			fmt.Printf("🗂️ Partition maintenance worker stopped\n")
			return
		}
	}
}

// maintainPartitions creates future partitions and drops old ones
func (s *logStore) maintainPartitions() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fmt.Printf("🗂️ Running partition maintenance...\n")

	// Create future partitions (7 months ahead by default)
	lookaheadMonths := s.config.PartitionLookaheadDays / 30
	if lookaheadMonths < 1 {
		lookaheadMonths = 1
	}

	if err := s.createFuturePartitions(ctx, "call_events", lookaheadMonths); err != nil {
		fmt.Printf("⚠️ Failed to create future partitions for call_events: %v\n", err)
	}

	if err := s.createFuturePartitions(ctx, "call_payloads", lookaheadMonths); err != nil {
		fmt.Printf("⚠️ Failed to create future partitions for call_payloads: %v\n", err)
	}

	// Drop old partitions based on retention policy
	retentionMonths := s.config.RetentionEventsDays / 30
	if err := s.dropOldPartitions(ctx, "call_events", retentionMonths); err != nil {
		fmt.Printf("⚠️ Failed to drop old partitions for call_events: %v\n", err)
	}

	retentionMonths = s.config.RetentionPayloadsDays / 30
	if err := s.dropOldPartitions(ctx, "call_payloads", retentionMonths); err != nil {
		fmt.Printf("⚠️ Failed to drop old partitions for call_payloads: %v\n", err)
	}

	fmt.Printf("✅ Partition maintenance complete\n")
}

// createFuturePartitions creates monthly partitions ahead
func (s *logStore) createFuturePartitions(ctx context.Context, tableName string, months int) error {
	now := time.Now().UTC()

	for i := 0; i <= months; i++ {
		targetMonth := now.AddDate(0, i, 0)

		// Get first day of target month
		startOfMonth := time.Date(targetMonth.Year(), targetMonth.Month(), 1, 0, 0, 0, 0, time.UTC)
		// Get first day of next month
		endOfMonth := startOfMonth.AddDate(0, 1, 0)

		partitionName := fmt.Sprintf("%s_%s", tableName, startOfMonth.Format("2006_01"))

		// CREATE TABLE IF NOT EXISTS call_events_2026_01 PARTITION OF call_events
		// FOR VALUES FROM ('2026-01-01') TO ('2026-02-01')
		query := fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s PARTITION OF %s
			FOR VALUES FROM ('%s') TO ('%s')
		`, partitionName, tableName,
			startOfMonth.Format("2006-01-02"),
			endOfMonth.Format("2006-01-02"))

		if _, err := s.pool.Exec(ctx, query); err != nil {
			// Log but continue (partition might already exist)
			fmt.Printf("⚠️ Failed to create partition %s: %v\n", partitionName, err)
			continue
		}

		// Only log creation for partitions >= current month
		switch i {
		case 0:
			fmt.Printf("📅 Partition ready: %s (%s to %s)\n",
				partitionName,
				startOfMonth.Format("2006-01-02"),
				endOfMonth.Format("2006-01-02"))
		case 1:
			fmt.Printf("📅 Created partition: %s (next month)\n", partitionName)
		}
	}

	return nil
}

// dropOldPartitions drops partitions older than retention period
func (s *logStore) dropOldPartitions(ctx context.Context, tableName string, retentionMonths int) error {
	now := time.Now().UTC()

	// Calculate cutoff month
	cutoffDate := now.AddDate(0, -retentionMonths, 0)
	cutoffMonth := time.Date(cutoffDate.Year(), cutoffDate.Month(), 1, 0, 0, 0, 0, time.UTC)

	// Only drop partitions older than cutoff
	// We'll check partitions from 3 years ago to cutoff month
	for i := 36; i > 0; i-- {
		targetMonth := now.AddDate(0, -i, 0)
		if targetMonth.After(cutoffMonth) || targetMonth.Equal(cutoffMonth) {
			continue // Don't drop partitions within retention period
		}

		startOfMonth := time.Date(targetMonth.Year(), targetMonth.Month(), 1, 0, 0, 0, 0, time.UTC)
		partitionName := fmt.Sprintf("%s_%s", tableName, startOfMonth.Format("2006_01"))

		// DROP TABLE IF EXISTS call_events_2023_01
		query := fmt.Sprintf("DROP TABLE IF EXISTS %s", partitionName)

		if _, err := s.pool.Exec(ctx, query); err != nil {
			fmt.Printf("⚠️ Failed to drop partition %s: %v\n", partitionName, err)
			continue
		}

		fmt.Printf("🗑️ Dropped old partition: %s (older than %d months)\n", partitionName, retentionMonths)
	}

	return nil
}

// GetPartitionInfo returns partition information for a table
func (s *logStore) GetPartitionInfo(ctx context.Context, tableName string) ([]PartitionInfo, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool not initialized")
	}

	query := `
		SELECT
			schemaname,
			tablename,
			pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS size,
			COALESCE(n_live_tup, 0) AS row_count
		FROM pg_stat_user_tables
		WHERE tablename LIKE $1 || '_%'
		ORDER BY tablename DESC
	`

	rows, err := s.pool.Query(ctx, query, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var partitions []PartitionInfo
	for rows.Next() {
		var p PartitionInfo
		if err := rows.Scan(&p.SchemaName, &p.TableName, &p.Size, &p.RowCount); err != nil {
			return nil, err
		}
		partitions = append(partitions, p)
	}

	return partitions, rows.Err()
}

// PartitionInfo holds partition metadata
type PartitionInfo struct {
	SchemaName string
	TableName  string
	Size       string
	RowCount   int64
}

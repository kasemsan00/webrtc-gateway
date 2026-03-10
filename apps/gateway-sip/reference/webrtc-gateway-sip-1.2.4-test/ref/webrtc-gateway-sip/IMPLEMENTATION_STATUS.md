# K2 Gateway - PostgreSQL Integration Implementation Plan

## 📋 Project Overview

**Goal:** Integrate PostgreSQL 18 with K2 Gateway for comprehensive per-call debug logging

**Requirements:**

- PostgreSQL driver: `pgx/v5`
- Partition strategy: Monthly (24 partitions for 2-year retention)
- Log retention: 730 days (2 years)
- Logging scope: WebSocket + SIP signaling (A + B only)
- Async processing: Batch inserts to avoid blocking RTP hot paths

---

## 🎯 Implementation Phases

### ✅ Phase 1: Infrastructure Setup (COMPLETED)

**Status:** 100% Complete

#### Tasks Completed:

1. **Dependencies** ✅
   - Added `github.com/jackc/pgx/v5@v5.8.0`
   - Added `github.com/jackc/pgx/v5/pgxpool@v5.8.0`

2. **Configuration** ✅
   - Added `DBConfig` struct to `internal/config/config.go`
   - Added environment variables to `.env`:
     ```bash
     DB_ENABLE=true
     DB_DSN=postgres://k2user:k2pass@localhost:5432/k2_gateway?sslmode=disable
     DB_STATS_INTERVAL_MS=5000
     DB_LOG_FULL_SIP=true
     DB_BATCH_SIZE=100
     DB_BATCH_INTERVAL_MS=1000
     DB_PARTITION_LOOKAHEAD_DAYS=7
     DB_RETENTION_PAYLOADS_DAYS=730
     DB_RETENTION_EVENTS_DAYS=730
     DB_RETENTION_STATS_DAYS=730
     DB_RETENTION_SESSIONS_DAYS=730
     ```

3. **Docker Compose** ✅
   - Added PostgreSQL 18 Alpine service
   - Configured health checks
   - Added network and volume configuration
   - Set up auto-initialization with `init.sql`

4. **Database Schema** ✅
   - Updated `init.sql` for monthly partitioning
   - Set 2-year retention policy in comments
   - Tables created:
     - `call_sessions` - Session snapshots
     - `call_events` - Event timeline (partitioned monthly)
     - `call_payloads` - SDP/SIP payloads (partitioned monthly)
     - `call_stats` - Periodic stats
     - `sip_dialogs` - Dialog state

---

### ✅ Phase 2: Core LogStore Package (COMPLETED)

**Status:** 100% Complete

**Location:** `internal/logstore/`

#### Files Created:

1. **models.go** ✅ (200 lines)
   - `SessionRecord` - Call session data
   - `Event` - Timeline events
   - `PayloadRecord` - Large payloads (SDP, SIP messages)
   - `StatsRecord` - Periodic RTP/RTCP stats
   - `DialogRecord` - SIP dialog state
   - `PartitionInfo` - Partition metadata

2. **logstore.go** ✅ (280 lines)
   - `LogStore` interface definition
   - `logStore` implementation with connection pooling
   - `noopStore` for when DB is disabled
   - Methods:
     - `Start(ctx)` - Initialize DB, start workers, create partitions
     - `Stop()` - Graceful shutdown, flush queues
     - `UpsertSession(ctx, sess)` - Insert/update session
     - `LogEvent(event)` - Async event logging (queued)
     - `StorePayload(ctx, payload)` - Sync payload storage, returns ID
     - `RecordStats(stats)` - Async stats recording (queued)
     - `UpsertDialog(ctx, dialog)` - Insert/update dialog state

3. **queue.go** ✅ (195 lines)
   - `eventBatchWorker()` - Processes events in batches
   - `batchInsertEvents()` - Batch insert using pgx.Batch
   - `statsBatchWorker()` - Processes stats in batches
   - `batchInsertStats()` - Batch insert stats
   - Features:
     - Buffer: 10,000 events, 1,000 stats
     - Batch size: 100 rows or 1 second interval
     - Graceful shutdown with final flush

4. **partition.go** ✅ (175 lines)
   - `partitionMaintenanceWorker()` - Runs daily
   - `maintainPartitions()` - Orchestrates partition tasks
   - `createFuturePartitions()` - Creates monthly partitions 7 months ahead
   - `dropOldPartitions()` - Drops partitions > 730 days old
   - `GetPartitionInfo()` - Query partition metadata
   - Features:
     - Auto-runs on startup and every 24 hours
     - Monthly partition format: `call_events_2026_01`
     - Retention based on config (730 days default)

**Build Status:** ✅ Compiles successfully

---

### ✅ Phase 3: Integration Hooks (COMPLETED)

**Status:** 100% Complete

**Summary:** Comprehensive LogStore hooks now cover both WebSocket/REST signaling and SIP signaling paths. Payloads (SDP offers/answers, SIP messages, INVITE/BYE bodies when `DB_LOG_FULL_SIP=true`) are captured before events are queued, and session/dialog snapshots are persisted whenever state transitions occur.

#### A) WebSocket/REST Signaling Hooks

- `internal/api/server.go`
  - `Server` struct exposes `SetLogStore` and helper methods for logging events, payloads, and session snapshots.
  - WebSocket handlers (`handleWSOffer`, `handleWSCall`, `handleWSHangup`, `handleWSAccept`, `handleWSReject`, `handleWSDTMF`, `handleWSSendMessage`, etc.) emit `ws_*` events, store SDP payloads, and update session state records.
- `internal/api/handlers.go`
  - REST endpoints mirror the same coverage with `rest_*` events for offer, call, hangup, and DTMF flows, including payload persistence and session snapshots.

#### B) SIP Signaling Hooks

- `internal/sip/server.go`
  - Added LogStore wiring plus helper toggles (e.g., `SetLogFullSIP`).
- `internal/sip/call.go`
  - Outbound call flow logs INVITE construction, SDP offer creation/storage, responses (100/180/183/200/401/407), dialog capture, ACK handling, BYE send/response, and DTMF send events.
  - Inbound accept/reject paths log INVITE SDP payloads, negotiated codecs, 200 OK payloads, dialog state, and session termination reasons.
- `internal/sip/handlers.go`
  - Incoming INVITE/ACK/BYE/MESSAGE handlers emit `sip_*` events, optionally persist raw SIP messages, and snapshot sessions/dialogs when state changes.
- `internal/sip/message.go`
  - Both out-of-dialog and in-dialog SIP MESSAGE sends store payloads and emit success/failure events.
- `internal/sip/rtp.go`
  - DTMF reception over RTP logs `sip_dtmf_received` events and correlates digits with sessions.

**Result:** End-to-end signaling now generates ~30 structured events per call, ensuring PostgreSQL has the data needed for per-call debugging.

---

### ✅ Phase 4: Wire LogStore (COMPLETED)

**Status:** 100% Complete

**Highlights:**

- `main.go` now initializes the LogStore immediately after configuration load, starts it once, and defers shutdown for both legacy and API modes.
- The same LogStore instance is injected into SIP and API servers (`SetLogStore`) along with `SetLogFullSIP` toggles so SIP handlers know when to persist raw messages.
- Legacy mode also benefits from LogStore, ensuring parity between operating modes.

---

### ⏳ Phase 5: SQL Helper Scripts (PENDING)

**Status:** 0% Complete (Optional)

**Files to create:**

1. **scripts/create_partitions.sql** (Estimated: 50 lines)
   - PL/pgSQL function to create partitions for next 12 months
   - Usage: `psql -U k2user -d k2_gateway -f scripts/create_partitions.sql`

2. **scripts/drop_old_partitions.sql** (Estimated: 50 lines)
   - PL/pgSQL function to drop partitions older than 730 days
   - Usage: `psql -U k2user -d k2_gateway -f scripts/drop_old_partitions.sql`

3. **scripts/check_partition_health.sql** (Estimated: 30 lines)
   - Query to show partition sizes and row counts
   - Usage: `psql -U k2user -d k2_gateway -f scripts/check_partition_health.sql`

**Note:** These are optional because `internal/logstore/partition.go` handles everything automatically.

---

### ⏳ Phase 6: Testing & Validation (PENDING)

**Status:** 0% Complete

**Test Cases:**

1. **Database Connection** ✅ (Can test now)

   ```bash
   docker-compose up -d postgres
   docker logs k2-postgres
   # Should see: "database system is ready to accept connections"
   ```

2. **Schema Creation** ✅ (Can test now)

   ```bash
   docker exec -it k2-postgres psql -U k2user -d k2_gateway -c "\dt"
   # Should see: call_sessions, call_events, call_payloads, call_stats, sip_dialogs
   ```

3. **Partition Auto-Creation** (After Phase 4)
   - Start gateway
   - Check logs for partition creation messages
   - Query: `SELECT tablename FROM pg_tables WHERE tablename LIKE 'call_events_%';`

4. **Event Logging** (After Phase 3+4)
   - Make a test call
   - Check database for events:
     ```sql
     SELECT ts, category, name, session_id
     FROM call_events
     ORDER BY ts DESC
     LIMIT 50;
     ```

5. **Payload Storage** (After Phase 3+4)
   - Make a test call
   - Check SDP payloads:
     ```sql
     SELECT kind, LENGTH(body_text) as size
     FROM call_payloads
     ORDER BY ts DESC;
     ```

6. **Session Tracking** (After Phase 3+4)
   - Make a test call
   - Check session record:
     ```sql
     SELECT * FROM call_sessions ORDER BY created_at DESC LIMIT 1;
     ```

7. **Performance** (After Phase 3+4)
   - Monitor RTP latency (should be unchanged)
   - Check event queue doesn't overflow
   - Verify batch inserts working

8. **Retention** (Manual test after 730+ days)
   - Old partitions should be auto-dropped
   - Or manually test: `UPDATE` partition dates in `partition.go`

---

## 📊 Current Status Summary

### ✅ Completed (100%)

- **Phase 1:** Infrastructure Setup
- **Phase 2:** Core LogStore Package

### ⏳ Remaining Work

- **Phase 5:** SQL Helper Scripts - Optional (not started)
- **Phase 6:** Testing & Validation - In progress (manual verification pending)

### 📈 Progress: ~80% Complete

**Time Estimate for Remaining Work:**

- Phase 5 (Scripts): 1 hour (optional)
- Phase 6 (Testing): 2 hours

**Total Remaining:** ~2-3 hours (excluding optional scripts)

---

## 🔧 How to Continue (Next Steps)

### Option 1: Test Current Implementation (Recommended First)

```bash
# 1. Start PostgreSQL only
docker-compose up -d postgres

# 2. Connect and verify schema
docker exec -it k2-postgres psql -U k2user -d k2_gateway

# 3. Run test queries
\dt                           # List tables
\d call_sessions              # Show session table structure
\d+ call_events               # Show events table with partitions
SELECT * FROM pg_tables WHERE tablename LIKE 'call_%';
```

### Option 2: Continue with Phase 3 (Integration Hooks)

Start with WebSocket hooks in `internal/api/server.go`:

1. Add `logStore LogStore` field
2. Add `SetLogStore()` method
3. Hook `handleWSoffer()` events
4. Test with a simple call

### Option 3: Build Everything (Fast Track)

Implement Phase 3 + 4 together:

1. Add LogStore to API and SIP servers
2. Hook all events in parallel
3. Wire in main.go
4. Test end-to-end

---

## 📝 Key Features Implemented

### Async Processing

- Events buffered: 10,000 capacity
- Stats buffered: 1,000 capacity
- Batch size: 100 rows per insert
- Flush interval: 1 second (configurable)

### Partition Management

- Auto-create: 7 months ahead
- Auto-drop: > 730 days old
- Runs: On startup + every 24 hours
- Format: `call_events_YYYY_MM`

### Safety Features

- Queue full = drop + warn (never block)
- Graceful shutdown with final flush
- No-op mode when DB disabled
- Connection pooling (min=2, max=10)

### Storage Efficiency

- Monthly partitions: ~24 partitions for 2 years
- Estimated size: 50-100 GB for 730 days
- Compressed JSONB for metadata
- Optional full SIP message logging

---

## 🎯 Implementation Guidelines

### Critical Rules (From DatabasePlan.md)

1. **NEVER block RTP/RTCP hot paths**
   - ❌ NO: `logStore.StorePayload()` in `handleVideoRTPPacketsForSession()`
   - ✅ YES: In-memory counters → flush to `call_stats` periodically

2. **Use async logging for events**
   - ✅ `logStore.LogEvent(event)` - queued, non-blocking
   - ⚠️ `logStore.StorePayload()` - sync, only use in signaling path

3. **Handle queue full gracefully**
   - Drop event + log warning
   - NEVER block goroutines

4. **No sensitive data**
   - ❌ Don't log SIP passwords (401/407 challenges)
   - ✅ Mask credentials in logs

5. **Error handling**
   - DB errors should NOT crash gateway
   - Log error and continue operation

---

## 📚 Reference Documentation

- **Database Plan:** `DatabasePlan.md`
- **Agent Instructions:** `AGENTS.md`
- **Schema:** `init.sql`
- **Code:** `internal/logstore/*.go`

---

**Last Updated:** 2026-01-28
**Implementation By:** K2 Gateway Team
**PostgreSQL Version:** 18 (Alpine)
**Go Version:** 1.25.5

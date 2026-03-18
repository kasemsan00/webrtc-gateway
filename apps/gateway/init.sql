-- K2 Gateway - Postgres Database Schema
-- Per-Call Debug Logging (Full Trace)
--
-- Design:
--   - Append-only event log for timeline reconstruction
--   - Separate payloads table to reduce bloat
--   - Partitioned by MONTH for efficient retention management (2-year retention)
--   - Supports full SIP/SDP/WebRTC debugging

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ============================================================================
-- 1) One row per internal session
-- ============================================================================
CREATE TABLE IF NOT EXISTS call_sessions (
  session_id        TEXT PRIMARY KEY,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  ended_at          TIMESTAMPTZ,

  direction         TEXT NOT NULL CHECK (direction IN ('inbound','outbound')),
  from_uri          TEXT,
  to_uri            TEXT,

  sip_call_id       TEXT,
  final_state       TEXT,
  end_reason        TEXT,

  rtp_audio_port    INT,
  rtp_video_port    INT,
  rtcp_audio_port   INT,
  rtcp_video_port   INT,

  sip_opus_pt       INT,
  audio_profile     TEXT,
  video_profile     TEXT,
  video_rejected    BOOLEAN,

  meta              JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_call_sessions_sip_call_id ON call_sessions (sip_call_id);
CREATE INDEX IF NOT EXISTS idx_call_sessions_created_at ON call_sessions (created_at DESC);


-- ============================================================================
-- 2) Append-only event timeline (partitioned)
-- ============================================================================
CREATE TABLE IF NOT EXISTS call_events (
  id               BIGSERIAL,
  ts               TIMESTAMPTZ NOT NULL DEFAULT now(),
  session_id       TEXT NOT NULL,

  category         TEXT NOT NULL,
  name             TEXT NOT NULL,

  -- Common searchable fields
  sip_method       TEXT,
  sip_status_code  INT,
  sip_call_id      TEXT,
  state            TEXT,

  payload_id       BIGINT,
  data             JSONB NOT NULL DEFAULT '{}'::jsonb,

  PRIMARY KEY (id, ts)
) PARTITION BY RANGE (ts);

CREATE INDEX IF NOT EXISTS idx_call_events_session_ts ON call_events (session_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_call_events_name_ts ON call_events (name, ts DESC);
CREATE INDEX IF NOT EXISTS idx_call_events_sip_call_id ON call_events (sip_call_id);


-- ============================================================================
-- 3) Big payloads (SDP, full SIP msg, errors) (partitioned)
-- ============================================================================
CREATE TABLE IF NOT EXISTS call_payloads (
  payload_id       BIGSERIAL,
  ts               TIMESTAMPTZ NOT NULL DEFAULT now(),
  session_id       TEXT NOT NULL,

  kind             TEXT NOT NULL,  -- webrtc_sdp_offer, webrtc_sdp_answer, sip_sdp_offer, sip_sdp_answer, sip_message, error_blob
  content_type     TEXT,
  body_text        TEXT,
  body_bytes_b64   TEXT,
  parsed           JSONB NOT NULL DEFAULT '{}'::jsonb,

  PRIMARY KEY (payload_id, ts)
) PARTITION BY RANGE (ts);

CREATE INDEX IF NOT EXISTS idx_call_payloads_session_ts ON call_payloads (session_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_call_payloads_kind_ts ON call_payloads (kind, ts DESC);


-- ============================================================================
-- 4) Periodic stats snapshots
-- ============================================================================
CREATE TABLE IF NOT EXISTS call_stats (
  id               BIGSERIAL PRIMARY KEY,
  ts               TIMESTAMPTZ NOT NULL DEFAULT now(),
  session_id       TEXT NOT NULL,

  pli_sent         INT,
  pli_response     INT,
  last_pli_sent_at TIMESTAMPTZ,
  last_keyframe_at TIMESTAMPTZ,

  audio_rtcp_rr    INT,
  audio_rtcp_sr    INT,
  video_rtcp_rr    INT,
  video_rtcp_sr    INT,

  data             JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_call_stats_session_ts ON call_stats (session_id, ts DESC);


-- ============================================================================
-- 5) Optional: SIP dialog state snapshots (typed)
-- ============================================================================
CREATE TABLE IF NOT EXISTS sip_dialogs (
  id               BIGSERIAL PRIMARY KEY,
  session_id       TEXT NOT NULL,
  ts               TIMESTAMPTZ NOT NULL DEFAULT now(),

  sip_call_id      TEXT,
  from_tag         TEXT,
  to_tag           TEXT,
  remote_contact   TEXT,
  cseq             INT,
  route_set        JSONB NOT NULL DEFAULT '[]'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_sip_dialogs_session_ts ON sip_dialogs (session_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_sip_dialogs_call_id ON sip_dialogs (sip_call_id);


-- ============================================================================
-- 6) SIP Trunks (for auto-register + scale with lease)
-- ============================================================================
CREATE TABLE IF NOT EXISTS sip_trunks (
  id                BIGSERIAL PRIMARY KEY,
  public_id         UUID NOT NULL DEFAULT gen_random_uuid(),
  name              TEXT NOT NULL UNIQUE,
  domain            TEXT NOT NULL,
  port              INT NOT NULL DEFAULT 5060 CHECK (port BETWEEN 1 AND 65535),
  username          TEXT NOT NULL,
  password          TEXT NOT NULL,  -- WARNING: Encrypt in production or restrict DB access
  transport         TEXT NOT NULL DEFAULT 'tcp' CHECK (transport IN ('tcp', 'udp')),
  enabled           BOOLEAN NOT NULL DEFAULT true,
  is_default        BOOLEAN NOT NULL DEFAULT false,
  
  -- Lease fields for single-active per trunk (scale)
  lease_owner       TEXT,           -- instance_id that owns this trunk
  lease_until       TIMESTAMPTZ,    -- lease expiry
  
  -- Status
  last_registered_at TIMESTAMPTZ,
  last_error         TEXT,

  -- Active call tracking: set to JWT subject when a call starts, cleared on hangup
  in_use_by          TEXT,

  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_sip_trunks_enabled ON sip_trunks (enabled) WHERE enabled = true;
CREATE INDEX IF NOT EXISTS idx_sip_trunks_lease ON sip_trunks (lease_owner, lease_until);
CREATE INDEX IF NOT EXISTS idx_sip_trunks_default ON sip_trunks (is_default) WHERE is_default = true;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sip_trunks_public_id ON sip_trunks (public_id);
CREATE INDEX IF NOT EXISTS idx_sip_trunks_name ON sip_trunks (name);
CREATE INDEX IF NOT EXISTS idx_sip_trunks_username ON sip_trunks (username);
CREATE INDEX IF NOT EXISTS idx_sip_trunks_enabled_default ON sip_trunks (enabled, is_default);
CREATE INDEX IF NOT EXISTS idx_sip_trunks_in_use_by ON sip_trunks (in_use_by) WHERE in_use_by IS NOT NULL;


-- ============================================================================
-- 7) Session Directory (for resume_redirect across instances)
-- ============================================================================
CREATE TABLE IF NOT EXISTS session_directory (
  session_id        TEXT PRIMARY KEY,
  owner_instance_id TEXT NOT NULL,
  ws_url            TEXT NOT NULL,  -- WebSocket URL to reconnect (e.g., wss://gw-1.../ws)
  expires_at        TIMESTAMPTZ NOT NULL,
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_session_directory_expires ON session_directory (expires_at);
CREATE INDEX IF NOT EXISTS idx_session_directory_owner ON session_directory (owner_instance_id);


-- ============================================================================
-- 8) Gateway Instances (for trunk redirect + service discovery)
-- ============================================================================
CREATE TABLE IF NOT EXISTS gateway_instances (
  instance_id       TEXT PRIMARY KEY,
  ws_url            TEXT NOT NULL,  -- WebSocket URL to reconnect (e.g., wss://gw-1.../ws)
  expires_at        TIMESTAMPTZ NOT NULL,
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_gateway_instances_expires ON gateway_instances (expires_at);


-- ============================================================================
-- Notes:
-- ============================================================================
-- 1) Partition Management (Monthly):
--    - call_events and call_payloads use monthly partitions
--    - Partitions are auto-created by the gateway (7 months ahead)
--    - Example: CREATE TABLE call_events_2026_01 PARTITION OF call_events
--               FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
--
-- 2) Retention Strategy (2 years = 730 days):
--    - call_payloads: 730 days (~24 monthly partitions)
--    - call_events: 730 days (~24 monthly partitions)
--    - call_stats: 730 days
--    - call_sessions: 730 days
--    Auto-drop old partitions with: DROP TABLE call_events_2024_01;
--
-- 3) Initial Partitions:
--    The gateway will create partitions automatically on startup.
--    For manual creation, see scripts/create_partitions.sql
--
-- 4) Performance:
--    - NEVER insert synchronously in RTP/RTCP hot paths
--    - Use async worker queue + batch inserts (100 rows per batch)
--    - Aggregate media metrics in-memory, flush periodically to call_stats (every 5s)
--
-- 5) SIP Trunks:
--    - lease_owner + lease_until implement single-active per trunk (scale)
--    - Only one instance holds lease per trunk at a time
--    - Lease TTL managed by gateway (e.g., 60s, renewed every 20s)
--    - password field should be encrypted or access-controlled in production
--
-- 6) Session Directory:
--    - Enables resume_redirect when client reconnects to different instance
--    - TTL managed by gateway (e.g., 2-6 hours, longer than typical call)
--    - Cleanup: DELETE FROM session_directory WHERE expires_at < now()

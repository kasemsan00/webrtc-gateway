-- Add SIP authentication context columns to call_sessions table
-- This allows filtering and reporting by trunk and auth mode

ALTER TABLE call_sessions
  ADD COLUMN IF NOT EXISTS auth_mode TEXT,
  ADD COLUMN IF NOT EXISTS trunk_id BIGINT,
  ADD COLUMN IF NOT EXISTS trunk_name TEXT,
  ADD COLUMN IF NOT EXISTS sip_username TEXT;

-- Add index on trunk_id for filtering by trunk
CREATE INDEX IF NOT EXISTS idx_call_sessions_trunk_id ON call_sessions(trunk_id);

-- Add index on auth_mode for filtering
CREATE INDEX IF NOT EXISTS idx_call_sessions_auth_mode ON call_sessions(auth_mode);

COMMENT ON COLUMN call_sessions.auth_mode IS 'SIP authentication mode: "public" | "trunk" | ""';
COMMENT ON COLUMN call_sessions.trunk_id IS 'Foreign key to sip_trunks.id (nullable)';
COMMENT ON COLUMN call_sessions.trunk_name IS 'Snapshot of trunk name at call time (denormalized)';
COMMENT ON COLUMN call_sessions.sip_username IS 'SIP username used for the call';

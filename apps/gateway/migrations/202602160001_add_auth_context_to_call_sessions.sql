-- +goose Up
-- Add SIP authentication context columns to call_sessions table.

ALTER TABLE call_sessions
  ADD COLUMN IF NOT EXISTS auth_mode TEXT,
  ADD COLUMN IF NOT EXISTS trunk_id BIGINT,
  ADD COLUMN IF NOT EXISTS trunk_name TEXT,
  ADD COLUMN IF NOT EXISTS sip_username TEXT;

CREATE INDEX IF NOT EXISTS idx_call_sessions_trunk_id ON call_sessions(trunk_id);
CREATE INDEX IF NOT EXISTS idx_call_sessions_auth_mode ON call_sessions(auth_mode);

COMMENT ON COLUMN call_sessions.auth_mode IS 'SIP authentication mode: public | trunk | empty';
COMMENT ON COLUMN call_sessions.trunk_id IS 'Foreign key to sip_trunks.id (nullable)';
COMMENT ON COLUMN call_sessions.trunk_name IS 'Snapshot of trunk name at call time (denormalized)';
COMMENT ON COLUMN call_sessions.sip_username IS 'SIP username used for the call';

-- +goose Down
DROP INDEX IF EXISTS idx_call_sessions_trunk_id;
DROP INDEX IF EXISTS idx_call_sessions_auth_mode;

ALTER TABLE call_sessions
  DROP COLUMN IF EXISTS auth_mode,
  DROP COLUMN IF EXISTS trunk_id,
  DROP COLUMN IF EXISTS trunk_name,
  DROP COLUMN IF EXISTS sip_username;

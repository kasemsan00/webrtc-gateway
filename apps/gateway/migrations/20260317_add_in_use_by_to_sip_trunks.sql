-- Track which user is currently using each trunk for a call.
-- Safe to run multiple times.

ALTER TABLE sip_trunks
  ADD COLUMN IF NOT EXISTS in_use_by TEXT;

CREATE INDEX IF NOT EXISTS idx_sip_trunks_in_use_by
  ON sip_trunks (in_use_by)
  WHERE in_use_by IS NOT NULL;

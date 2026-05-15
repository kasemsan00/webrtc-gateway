-- +goose Up
-- Ensure existing databases have the iOS VoIP push Contact columns.

ALTER TABLE sip_trunks
  ADD COLUMN IF NOT EXISTS pn_app_id TEXT,
  ADD COLUMN IF NOT EXISTS pn_type TEXT,
  ADD COLUMN IF NOT EXISTS pn_token TEXT,
  ADD COLUMN IF NOT EXISTS pn_updated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_sip_trunks_pn_token
  ON sip_trunks (pn_token)
  WHERE pn_token IS NOT NULL;

-- +goose Down
-- No-op: this migration is a forward-only repair guard. The original
-- 202605140001 migration owns rollback for these columns.
SELECT 1;

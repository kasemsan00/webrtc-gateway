-- +goose Up
ALTER TABLE sip_trunks
  ADD COLUMN IF NOT EXISTS pn_app_id TEXT,
  ADD COLUMN IF NOT EXISTS pn_type TEXT,
  ADD COLUMN IF NOT EXISTS pn_token TEXT,
  ADD COLUMN IF NOT EXISTS pn_updated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_sip_trunks_pn_token
  ON sip_trunks (pn_token)
  WHERE pn_token IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_sip_trunks_pn_token;

ALTER TABLE sip_trunks
  DROP COLUMN IF EXISTS pn_updated_at,
  DROP COLUMN IF EXISTS pn_token,
  DROP COLUMN IF EXISTS pn_type,
  DROP COLUMN IF EXISTS pn_app_id;

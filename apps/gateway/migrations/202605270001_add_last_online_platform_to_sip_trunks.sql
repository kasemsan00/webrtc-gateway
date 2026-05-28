-- +goose Up
ALTER TABLE sip_trunks
  ADD COLUMN IF NOT EXISTS last_online_platform TEXT,
  ADD COLUMN IF NOT EXISTS last_online_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_sip_trunks_last_online_platform
  ON sip_trunks (last_online_platform)
  WHERE last_online_platform IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_sip_trunks_last_online_platform;

ALTER TABLE sip_trunks
  DROP COLUMN IF EXISTS last_online_at,
  DROP COLUMN IF EXISTS last_online_platform;

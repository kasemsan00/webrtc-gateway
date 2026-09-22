-- +goose Up
-- Store FCM registration tokens for /ws-agent-device sticky push.
ALTER TABLE sip_trunks
  ADD COLUMN IF NOT EXISTS fcm_token TEXT,
  ADD COLUMN IF NOT EXISTS fcm_updated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_sip_trunks_fcm_token
  ON sip_trunks (fcm_token)
  WHERE fcm_token IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_sip_trunks_fcm_token;

ALTER TABLE sip_trunks
  DROP COLUMN IF EXISTS fcm_updated_at,
  DROP COLUMN IF EXISTS fcm_token;

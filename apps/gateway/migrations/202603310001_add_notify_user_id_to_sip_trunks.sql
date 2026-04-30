-- +goose Up
-- Persist the Keycloak user ID (sub claim) for push notifications on incoming calls.

ALTER TABLE sip_trunks
  ADD COLUMN IF NOT EXISTS notify_user_id TEXT;

CREATE INDEX IF NOT EXISTS idx_sip_trunks_notify_user_id
  ON sip_trunks (notify_user_id)
  WHERE notify_user_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_sip_trunks_notify_user_id;

ALTER TABLE sip_trunks
  DROP COLUMN IF EXISTS notify_user_id;

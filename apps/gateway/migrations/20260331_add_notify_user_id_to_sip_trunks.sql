-- Persist the Keycloak user ID (sub claim) for push notifications on incoming calls.
-- Unlike in_use_by (cleared on hangup), notify_user_id persists across sessions
-- so the gateway can send FCM push even when the user is offline.
-- Safe to run multiple times.

ALTER TABLE sip_trunks
  ADD COLUMN IF NOT EXISTS notify_user_id TEXT;

CREATE INDEX IF NOT EXISTS idx_sip_trunks_notify_user_id
  ON sip_trunks (notify_user_id)
  WHERE notify_user_id IS NOT NULL;

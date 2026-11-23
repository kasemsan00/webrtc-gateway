-- +goose Up
-- Restore PostgreSQL HOT updates on sip_trunks lease heartbeats by removing
-- unused/redundant indexes, adding a partial composite credential lookup index,
-- and tuning fillfactor + per-table autovacuum thresholds.

DROP INDEX IF EXISTS idx_sip_trunks_lease;
DROP INDEX IF EXISTS idx_sip_trunks_name;
DROP INDEX IF EXISTS idx_sip_trunks_enabled_default;
DROP INDEX IF EXISTS idx_sip_trunks_last_online_platform;
DROP INDEX IF EXISTS idx_sip_trunks_pn_token;
DROP INDEX IF EXISTS idx_sip_trunks_fcm_token;
DROP INDEX IF EXISTS idx_session_directory_owner;

CREATE INDEX IF NOT EXISTS idx_sip_trunks_cred_lookup
  ON sip_trunks (username, domain, port)
  WHERE enabled = true;

ALTER TABLE sip_trunks SET (
  fillfactor = 70,
  autovacuum_vacuum_scale_factor = 0.05,
  autovacuum_analyze_scale_factor = 0.05
);

-- +goose Down
ALTER TABLE sip_trunks RESET (
  fillfactor,
  autovacuum_vacuum_scale_factor,
  autovacuum_analyze_scale_factor
);

DROP INDEX IF EXISTS idx_sip_trunks_cred_lookup;

CREATE INDEX IF NOT EXISTS idx_sip_trunks_lease ON sip_trunks (lease_owner, lease_until);
CREATE INDEX IF NOT EXISTS idx_sip_trunks_name ON sip_trunks (name);
CREATE INDEX IF NOT EXISTS idx_sip_trunks_enabled_default ON sip_trunks (enabled, is_default);
CREATE INDEX IF NOT EXISTS idx_sip_trunks_last_online_platform ON sip_trunks (last_online_platform) WHERE last_online_platform IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sip_trunks_pn_token ON sip_trunks (pn_token) WHERE pn_token IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sip_trunks_fcm_token ON sip_trunks (fcm_token) WHERE fcm_token IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_session_directory_owner ON session_directory (owner_instance_id);

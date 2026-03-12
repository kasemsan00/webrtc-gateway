-- Improve update/list performance and enforce port range at DB level.
-- Safe to run multiple times.

CREATE INDEX IF NOT EXISTS idx_sip_trunks_name ON sip_trunks (name);
CREATE INDEX IF NOT EXISTS idx_sip_trunks_username ON sip_trunks (username);
CREATE INDEX IF NOT EXISTS idx_sip_trunks_enabled_default ON sip_trunks (enabled, is_default);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'sip_trunks_port_range_chk'
  ) THEN
    ALTER TABLE sip_trunks
      ADD CONSTRAINT sip_trunks_port_range_chk
      CHECK (port BETWEEN 1 AND 65535);
  END IF;
END $$;

-- +goose Up
-- Add stable public UUID for SIP trunks.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

ALTER TABLE sip_trunks
  ADD COLUMN IF NOT EXISTS public_id UUID;

UPDATE sip_trunks
SET public_id = gen_random_uuid()
WHERE public_id IS NULL;

ALTER TABLE sip_trunks
  ALTER COLUMN public_id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_sip_trunks_public_id
  ON sip_trunks (public_id);

-- +goose Down
DROP INDEX IF EXISTS idx_sip_trunks_public_id;

ALTER TABLE sip_trunks
  DROP COLUMN IF EXISTS public_id;

-- +goose Up
ALTER TABLE sip_trunks
  ADD COLUMN IF NOT EXISTS sip_auto_register BOOLEAN NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE sip_trunks
  DROP COLUMN IF EXISTS sip_auto_register;

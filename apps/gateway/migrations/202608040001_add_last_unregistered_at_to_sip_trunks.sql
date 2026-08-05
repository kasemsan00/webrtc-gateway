-- +goose Up
ALTER TABLE sip_trunks
  ADD COLUMN IF NOT EXISTS last_unregistered_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE sip_trunks
  DROP COLUMN IF EXISTS last_unregistered_at;

-- +goose Up
UPDATE sip_trunks
SET sip_auto_register = false, updated_at = NOW()
WHERE last_registered_at IS NULL;

ALTER TABLE sip_trunks
  ALTER COLUMN sip_auto_register SET DEFAULT false;

-- +goose Down
ALTER TABLE sip_trunks
  ALTER COLUMN sip_auto_register SET DEFAULT true;

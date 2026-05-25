-- +goose Up
-- Store client diagnostics that are not tied to a call session.

CREATE TABLE IF NOT EXISTS client_diagnostic_events (
  id                 BIGSERIAL PRIMARY KEY,
  ts                 TIMESTAMPTZ NOT NULL DEFAULT now(),
  client_trace_id    TEXT,
  auth_subject       TEXT,
  auth_realm         TEXT,
  preferred_username TEXT,
  source             TEXT NOT NULL,
  level              TEXT NOT NULL,
  name               TEXT NOT NULL,
  app_version        TEXT,
  platform           TEXT,
  device_id_hash     TEXT,
  data               JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_client_diagnostic_events_ts
  ON client_diagnostic_events (ts DESC);

CREATE INDEX IF NOT EXISTS idx_client_diagnostic_events_trace_ts
  ON client_diagnostic_events (client_trace_id, ts DESC)
  WHERE client_trace_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_client_diagnostic_events_auth_subject_ts
  ON client_diagnostic_events (auth_subject, ts DESC)
  WHERE auth_subject IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS client_diagnostic_events;

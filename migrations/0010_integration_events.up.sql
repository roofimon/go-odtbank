CREATE TABLE integration_events (
    id BIGSERIAL PRIMARY KEY,
    transfer_id TEXT NOT NULL REFERENCES transfers(id),
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('scheduled','published','dead_lettered')) DEFAULT 'scheduled',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX integration_events_transfer_unique ON integration_events(transfer_id, event_type);

CREATE INDEX integration_events_due_idx ON integration_events(status, next_attempt_at);

ALTER TABLE transfers
    ADD COLUMN current_step TEXT NOT NULL DEFAULT 'created',
    ADD COLUMN compliance_status TEXT NOT NULL DEFAULT '',
    ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN last_error TEXT NOT NULL DEFAULT '';

CREATE INDEX transfers_saga_due_idx ON transfers(status, next_attempt_at);

CREATE TABLE account_reservations (
    transfer_id TEXT PRIMARY KEY REFERENCES transfers(id),
    account_id TEXT NOT NULL,
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    state TEXT NOT NULL CHECK (state IN ('reserved','captured','released')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE compliance_decisions (
    transfer_id TEXT PRIMARY KEY REFERENCES transfers(id),
    decision TEXT NOT NULL CHECK (decision IN ('approved','rejected')),
    decided_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE ledger_postings (
    transfer_id TEXT PRIMARY KEY REFERENCES transfers(id),
    posted_at TIMESTAMPTZ NOT NULL
);

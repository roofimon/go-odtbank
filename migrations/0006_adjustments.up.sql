CREATE TABLE adjustment_requests (
    id TEXT PRIMARY KEY,
    adjustment_type TEXT NOT NULL CHECK (adjustment_type IN ('manual','reversal')),
    status TEXT NOT NULL CHECK (status IN ('waiting_for_approval','approved','rejected')),
    account_id TEXT NOT NULL,
    direction TEXT NOT NULL,
    amount_minor BIGINT NOT NULL,
    fee_minor BIGINT NOT NULL DEFAULT 0,
    counterparty_account_id TEXT NOT NULL DEFAULT '',
    original_transfer_id TEXT,
    original_account_id TEXT,
    original_event_sequence BIGINT,
    reason TEXT NOT NULL,
    case_reference TEXT NOT NULL,
    created_by TEXT NOT NULL REFERENCES admins(id),
    reviewed_by TEXT REFERENCES admins(id),
    rejection_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    reviewed_at TIMESTAMPTZ
);
CREATE INDEX adjustment_requests_status_created_idx ON adjustment_requests(status, created_at);
CREATE UNIQUE INDEX adjustment_requests_active_transfer_reversal_idx
    ON adjustment_requests(original_transfer_id)
    WHERE original_transfer_id IS NOT NULL AND status <> 'rejected';
CREATE UNIQUE INDEX adjustment_requests_active_event_reversal_idx
    ON adjustment_requests(original_account_id, original_event_sequence)
    WHERE original_account_id IS NOT NULL AND original_event_sequence IS NOT NULL AND status <> 'rejected';

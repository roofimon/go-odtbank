DROP TABLE IF EXISTS ledger_postings;
DROP TABLE IF EXISTS compliance_decisions;
DROP TABLE IF EXISTS account_reservations;
DROP INDEX IF EXISTS transfers_saga_due_idx;
ALTER TABLE transfers
    DROP COLUMN last_error,
    DROP COLUMN next_attempt_at,
    DROP COLUMN attempt_count,
    DROP COLUMN compliance_status,
    DROP COLUMN current_step;

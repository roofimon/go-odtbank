CREATE TABLE IF NOT EXISTS account_snapshots (
    aggregate_id            TEXT NOT NULL PRIMARY KEY,
    balance_minor           BIGINT NOT NULL,
    reserved_balance_minor  BIGINT NOT NULL,
    available_balance_minor BIGINT NOT NULL,
    as_of_sequence          BIGINT NOT NULL CHECK (as_of_sequence >= 0),
    occurred_at             TIMESTAMPTZ NOT NULL
);

-- Append-only event store.
-- (aggregate_id, sequence) is the natural primary key and gives both
-- fast per-aggregate reads and free optimistic-concurrency on insert.

CREATE TABLE IF NOT EXISTS events (
    aggregate_id  TEXT         NOT NULL,
    sequence      BIGINT       NOT NULL,
    event_type    TEXT         NOT NULL,
    payload       JSONB        NOT NULL,
    occurred_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (aggregate_id, sequence)
);

CREATE INDEX IF NOT EXISTS events_aggregate_id_idx ON events (aggregate_id);
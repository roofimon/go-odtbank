# Snapshotting requirements

Snapshots are a **read optimization** for long account event streams. A snapshot is a materialized view of an account's state at a point in the stream; after it, only the tail of events is folded.

## Model
- Only **account streams** are snapshotted. Customer, transfer, and adjustment records are already material rows.
- A snapshot records `balance`, `reserved_balance`, `available_balance`, and the `as_of_sequence` it already includes.
- Current state = snapshot + replay of events whose `Version() > as_of_sequence`. With no snapshot, the full stream is replayed.
- The append-only log is **never compacted or shortened**; snapshots only reduce how many events a read must fold.

## Creation policy — threshold + on-read
- A snapshot is written when a stream length reaches a threshold; the threshold is configurable via `SNAPSHOT_THRESHOLD` (default 50) and applies lazily on read.
- A fresh snapshot is written the first time a long stream is read, then kept up to date.
- **One snapshot per aggregate** (the latest); an older snapshot simply means more tail events are folded, which is always correct.
- A snapshot with a stale (`≤` existing) `as_of_sequence` is ignored, so a slow writer cannot overwrite a newer snapshot.

## Invariants
- A snapshot + its tail must always equal a full replay of the stream, regardless of where the boundary falls.
- Snapshot writes are **best-effort**: a write failure never fails the read; the reader falls back to a full replay.
- Event-history endpoints still return the **entire** stream; `event_count` reflects the whole log, not the tail.
- The write paths (reserve, withdraw, post, release) continue to load the full stream; only state reads use snapshots.

## Storage
- Snapshots persist in the `account_snapshots` table (migration `0009`); no data backfill is required — the first read of any pre-existing long stream materializes its snapshot.

## Limits / non-goals
- No per-aggregate snapshot history or time-travel beyond the single latest snapshot.
- No compaction or deletion of the underlying event log.

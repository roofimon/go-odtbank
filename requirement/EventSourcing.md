# Event sourcing (core model) requirements

The bank is **event-sourced**: account balances are never stored as mutable rows. Each balance is rebuilt by replaying an append-only stream of account events.

## Event stream
- All account aggregates share one `events` table; the natural primary key is `(aggregate_id, sequence)`.
- The `(aggregate_id, sequence)` key gives per-aggregate ordering **and** free optimistic concurrency: an append at an occupied sequence returns `409` `ErrConcurrencyConflict` and is not retried automatically.
- Money is signed 64-bit minor units for one implicit two-decimal currency; multi-currency is not supported.

## Stored events and replay effects
| Event | Effect during replay |
|-------|----------------------|
| `AccountOpened` | Sets the initial balance. |
| `MoneyCredited` | Adds a deposit or destination-side transfer credit. |
| `MoneyDebited` | Subtracts a withdrawal, fee, or source-side transfer debit. |
| `FundsReserved` | Places a hold without changing the posted balance. |
| `ReservationCaptured` | Closes a hold after ledger posting. |
| `ReservationReleased` | Releases a hold when a transfer fails before posting. |

`available_balance = balance - reserved_balance`.

## Integration events
- After a successful transfer, a `TransferCompletedEvent` is enqueued on a **durable outbox**. The outbox row is appended in the **same transaction as the ledger posting**, so a crash after the money moves but before delivery leaves a `scheduled` row that the worker redelivers — no event is lost.
- The durable worker drains due rows, delivers them to subscribers, and on failure retries with **exponential backoff** (capped), **dead-lettering** a row after a configurable number of attempts. A panicking subscriber is recovered and recorded as a failed delivery (never crashes the process). An event with no subscriber stays `scheduled`, so it is retried once a consumer appears.
- Outbox rows live in the `integration_events` table (migration `0010`). Statuses: `scheduled` → `published` (delivered) or `dead_lettered` (retries exhausted). A dead-lettered row can be requeued. Enqueue is idempotent on `(transfer_id, event_type)`.
- The durable bus is **single-process and at-least-once**: it has no cross-process consumer group and no dedup on the consumer side, so a consumer must tolerate duplicate delivery. It still does not compact the event log.

## Immutability
- Events are immutable facts. There is no delete or compensate-at-the-source operation; reversal and correction are expressed as **new** events.

## Limits / non-goals
- Event payloads have no schema versioning or upcasting strategy.
- The complete event stream is retained; reads historically replay the whole log (see `Snapshots.md`).

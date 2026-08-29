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
- After a successful transfer, a `TransferCompletedEvent` is published to the in-process event bus. It is **not** stored in account streams.
- The event bus is fire-and-forget: in-process, with no durable delivery, backpressure, or handler panic recovery.

## Immutability
- Events are immutable facts. There is no delete or compensate-at-the-source operation; reversal and correction are expressed as **new** events.

## Limits / non-goals
- Event payloads have no schema versioning or upcasting strategy.
- The complete event stream is retained; reads historically replay the whole log (see `Snapshots.md`).

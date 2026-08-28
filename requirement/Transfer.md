# Saga scenario in `go-odtbank`

The project runs a **durable, step-recoverable transfer saga**. Rather than moving money in one atomic transaction across two accounts, a transfer is broken into ordered steps that survive crashes, with the source account's funds held in a reservation throughout.

## Two phases: request vs. async work

**1. Synchronous request** — `TransferService.Transfer` (`transfer_service.go:36`)
- Validates amount (≥100), idempotency key, and service window.
- Computes the fee and persists a `TransferRecord` with `Status=pending`, `CurrentStep=created`, `NextAttemptAt=now`.
- Fires a non-blocking signal into `s.wake` and returns `202` immediately. The client polls `GET /transfers/{id}` until `completed`/`failed`.

**2. Background worker** — `Run` → `ProcessDue` → `Process` (`transfer_service.go:81`)
- A ticker (250ms, or the `wake` signal) drains `ListDueTransfers(now, 100)` and advances each due record one step at a time. Each step is a **separate transaction**, so the saga recovers from restarts purely from persisted state.

## The happy-path step machine

Each `Process` case appends events and moves `CurrentStep` forward (constants in `transfer.go:8`):

```
created ──ReserveTransferFunds──► funds_reserved
    │                                     │
    │   (FundsReserved event on source;     │
    │   hold added to ReservedBalance)      ▼
    │                          funds_reserved ──compliance──► compliance_approved
    │                                                         │
    │ (RecordComplianceDecision; if                │
    │  rejected → release + fail)                  │
    │                                               ▼
    │                          compliance_approved ──PostTransferLedger──► ledger_posted
    │                                                                   │
    │ (MoneyDebited amount + MoneyDebited fee on source,              │
    │  MoneyCredited on destination, atomically)                      ▼
    │                          ledger_posted ──CaptureTransferReservation──► reservation_captured
    │                                                                         │
    │ (ReservationCaptured event; hold closed)                     │
    │                                                              ▼
    │                                         UpdateTransferSaga → completed + EventBus.Publish
    ▼
```

- **`funds_reserved`**: `ReserveTransferFunds` checks `AvailableBalance` (excludes holds), appends `FundsReserved`, and records a reservation row. This is the hold that prevents double-spend.
- **`compliance_approved`**: compliance decision recorded; rejection releases the hold and fails the transfer.
- **`ledger_posted`**: the actual money move — source debit (amount + fee) and destination credit — is written **atomically in one tx** over both accounts (advisory locks in `PostTransferLedger`, `postgres_store.go:719`).
- **`reservation_captured`**: the hold is closed *after* posting, so a capture is retried until done rather than reversing money that's already moved.
- Final `UpdateTransferSaga` → `completed`, then publishes `TransferCompletedEvent` to the in-process `EventBus` (fire-and-forget, `event_bus.go:26`).

## Compensating actions and retry

`Process` distinguishes failure types, which drives compensation:

- **Pre-ledger failure** (`handleStepError`, `transfer_service.go:158`):
   - *Permanent* errors (`account not found`, *insufficient funds*) → release the reservation, mark `failed` with a code like `insufficient_funds`.
   - *Transient* errors → `retry` with exponential backoff (`1s << attempt`, up to 5 attempts); on exhaustion, release the hold and fail with `retry_exhausted`.
- **Post-ledger failure**: money is already posted, so on error it **retries the capture instead of reversing** ("once ledger posting succeeds, reservation capture is retried until it completes rather than reversing posted money" — `retry`, `transfer_service.go:173`).

## Key invariants
- **Reservation = the saga's compensation mechanism.** A held balance is visible as `reserved_balance`; `available_balance = balance - reserved_balance`, so concurrent transfers can't overspend.
- **Idempotency** is per `(source_account_id, idempotency_key)` — retries return the original record, different details → `409`.
- **Crash safety**: each step is its own committed transaction; `current_step` + `next_attempt_at` let the worker resume exactly where it stopped.
- **Event sourcing interplay**: the balance effects are recorded as account-stream events (`FundsReserved`/`ReservationCaptured` etc.); the saga's own steps live in the `transfers` / `account_reservations` / `compliance_decisions` / `ledger_postings` tables from migration `0008`.

In short: reserve → comply → post → capture, with the reservation compounding the pre-post steps and retry-till-success guarding the post-irreversibility boundary.

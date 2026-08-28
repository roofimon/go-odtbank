# Withdrawal requirements

A customer withdraws funds from their own account. The operation is event-sourced: a `MoneyDebited` event is appended to the account stream.

## `POST /withdraw`
- Body: `{account_id, amount}`.
- Requires a logged-in, approved customer session.
- Success returns `200` with `{initial_account, final_account, withdrawal_amount}`.

## Rules
- **Minimum withdrawal**: 10.00 (1000 minor units); below this returns `400` `ErrInvalidWithdrawAmount`.
- **Funds check**: a withdrawal may reduce the balance to exactly zero but cannot exceed the **available balance** (`balance - reserved_balance`). Overdraft returns `422` `InsufficientFundsError`.
- Account must exist; unknown account returns `404` `ErrAccountNotFound`.

## Atomicity (Postgres)
- When the store exposes `WithdrawAvailable`, the withdraw is committed atomically: the funds check and the event append happen in one transaction guarded by a per-account advisory lock, so concurrent withdrawals cannot overspend.
- The in-memory store falls back to a load-check-append path guarded by sequence numbers.

## Reservation interaction
- A withdrawal consumes **available** balance, so funds held by an in-flight transfer (a `Reservation`) are not available for withdrawal.

## Event effect
`MoneyDebited` subtracts `amount` from the posted balance during replay.

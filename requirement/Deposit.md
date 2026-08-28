# Deposit requirements

A customer deposits funds into their own existing account. The operation is event-sourced: a `MoneyCredited` event is appended to the account stream and the new balance is the replay of the stream plus this event.

## `POST /deposit`
- Body: `{account_id, amount}`.
- Requires a logged-in, approved customer session.
- Success returns `200` with `{initial_account, final_account, deposit_amount}`.

## Rules
- **Minimum deposit**: 10.00 (1000 minor units); below this returns `400` `ErrInvalidDepositAmount`.
- Account must exist; an unknown account returns `404` `ErrAccountNotFound`.
- A deposit has no upper bound and no reservation; it raises `balance` directly.
- The appended `MoneyCredited` event carries the amount and no transfer/fee metadata.

## Concurrency
- The append is guarded by per-aggregate sequence numbers; a concurrent append at the same sequence returns `409` `ErrConcurrencyConflict` and is not retried automatically.

## Event effect
`MoneyCredited` adds `amount` to the posted balance during replay.

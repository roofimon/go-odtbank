# Adjustment & reversal requirements

Administrators make manual balance corrections and reverse posted transactions. Every request starts in `waiting_for_approval` and must be **approved or rejected by a different administrator** (dual control).

## Request types
- **Manual correction** (`POST /admin/adjustments`): `{type: "manual", account_id, direction: credit|debit, amount, reason, case_reference}`.
- **Transfer reversal** (`POST /admin/adjustments`): `{type: "reversal", original_transfer_id, reason, case_reference}`. The reversal restores the original fee to the source account.

## Lifecycle
`waiting_for_approval` → (approve / reject) → terminal. Approval and rejection are one-time: a second review returns `409` `ErrAdjustmentReviewed`.
- `POST /admin/adjustments/{id}/approve` and `POST /admin/adjustments/{id}/reject {reason}`.
- An administrator **cannot approve or reject their own request** — that returns `409` `ErrSelfApproval`.
- A transfer can be reversed **only once**; a second reversal returns `409` `ErrAlreadyReversed`.

## Effects
- On approval, auditable ledger events are appended (`MoneyCredited`/`MoneyDebited`) carrying `case_reference` and a customer-visible reason, stamped with the adjustment id and original reference.
- A **debit** cannot overdraw an account (`InsufficientFundsError`).
- A manual reversal of an individual credit/debit is allowed only for customer-originated money movements — it cannot reverse a transfer leg, a fee, or an existing adjustment.
- A transfer reversal debits the counterparty by the original amount and credits the source by `amount + fee`.

## Concurrency
- Approval takes per-account advisory locks over the affected accounts; transfer reversals additionally re-check that no other active (non-rejected) reversal targets the same transfer or the same original event.

## Limits / non-goals
- Adjustment, reversal, transfer-workflow, reservation, compliance, and ledger-idempotency records are retained indefinitely.

# go-odtbank

An event-sourced banking demo built with Go, PostgreSQL, and Next.js. It supports public onboarding, session-based login, transfers, deposits, withdrawals, balances, and per-account transaction history.

Account balances are not stored as mutable rows. Each balance is rebuilt by replaying an append-only stream of `AccountOpened`, `MoneyCredited`, and `MoneyDebited` events.

## Features

- Submit an individual account application through a public three-step KYC onboarding journey.
- Review and approve or reject applications in a protected admin area.
- Create dual-control manual balance corrections and transaction reversals.
- Query any account's transaction history from the admin console.
- Log in with an email and password using an HTTP-only server-side session.
- Transfer money between existing accounts (minimum `1.00`).
- Deposit into an existing account (minimum `10.00`).
- Withdraw from an existing account (minimum `10.00`, subject to available funds).
- Run with an in-memory event store or PostgreSQL.
- Detect concurrent writes with per-aggregate sequence numbers.
- Browse accounts and their event history in a responsive Next.js dashboard.

## Architecture

```text
Next.js dashboard
        │ HTTP/JSON
        ▼
internal/httpapi
  router · handlers · DTOs · CORS · status mapping
        │
        ├── TransferService ── FeePolicy · TimeService · EventBus
        ├── DepositService
        ├── WithdrawService
        └── AdjustmentService
                 │
                 ▼
          eventstore.Store
          ├── MemoryStore
          └── PostgresStore
                 │
                 ▼
       append-only account streams
```

The domain package defines accounts, events, receipts, errors, and service contracts. Services load and replay account streams, validate commands, and append new events. `cmd/server` selects infrastructure, seeds demo accounts, wires dependencies, and starts the HTTP server.

### Project layout

```text
cmd/server/             Application entry point and dependency wiring
internal/domain/        Domain types, events, errors, and interfaces
internal/service/       Authentication, onboarding, transfer, deposit, and withdrawal use cases
internal/httpapi/       Router, handlers, DTOs, CORS, and HTTP error mapping
internal/eventstore/    In-memory and PostgreSQL event stores
internal/eventbus/      In-process TransferCompletedEvent publisher
internal/policy/        Fee and service-availability policies
internal/repository/    Event-replay account projection
migrations/             PostgreSQL schema migrations
web/                    Next.js dashboard and typed API client
```

## Requirements

- Go `1.27.0` or newer, as declared in `go.mod`.
- Node.js and npm for the dashboard.
- Docker with Docker Compose for the PostgreSQL development path.

## Quick start

### In-memory backend

No database setup is required:

```bash
go run ./cmd/server
```

The API starts at `http://localhost:8080`. When `DATABASE_URL` is unset, two accounts are seeded in memory:

| Account | Initial balance |
| ------- | --------------- |
| `acc1`  | `100.00`        |
| `acc2`  | `50.00`         |

Restarting the in-memory server resets all events, customers, and sessions. The seeded accounts have no login credentials; use onboarding to create a customer account for the dashboard.

### Dashboard

In a second terminal:

```bash
cd web
npm install
npm run dev
```

Open `http://localhost:3000/onboarding` to submit an application, then log in at `http://localhost:3000/login`. Customers see a waiting page until an administrator approves them. Approval creates the account and activates Transfer, Deposit, Withdraw, and Transaction history.

For an in-memory demo, create the development admin by setting both variables when the backend starts:

```bash
ADMIN_EMAIL='admin@example.com' \
ADMIN_PASSWORD='change-this-password' \
go run ./cmd/server
```

If the backend is already running, stop it and run the command above again. Admin variables are read only at startup. Log in at `http://localhost:3000/login` with `admin@example.com` and `change-this-password`, then open `/admin` to approve the customer application. The two automatically seeded accounts, `acc1` and `acc2`, are backend fixtures and cannot log in.

The browser calls `http://localhost:8080` by default. Override it when needed:

```bash
NEXT_PUBLIC_API_URL='https://api.example.com' npm run dev
```

The backend accepts credentialed browser requests through its CORS middleware. Set `CORS_ORIGINS` to a comma-separated origin allowlist; when unset, it reflects the request origin. Set `COOKIE_SECURE=true` when serving the API over HTTPS.

## PostgreSQL development

Start PostgreSQL and apply the migration:

```bash
docker compose up -d postgres
docker compose run --rm migrate up
```

Run the backend against it:

```bash
DATABASE_URL='postgres://postgres:postgres@localhost:5432/odtbank?sslmode=disable' \
  go run ./cmd/server
```

Equivalent Make targets:

| Command             | Purpose                                      |
| ------------------- | -------------------------------------------- |
| `make up`           | Start PostgreSQL.                            |
| `make migrate`      | Apply all migrations.                        |
| `make migrate-down` | Roll back one migration.                     |
| `make run`          | Run the backend using the default local DSN. |
| `make admin`        | Create or update an admin using `ADMIN_*`.   |
| `make down`         | Stop the containers.                         |

PostgreSQL persists data in the `postgres-data` Docker volume. Use `docker compose down -v` only when you also want to remove that data.

Create or update a PostgreSQL admin after applying all migrations:

```bash
DATABASE_URL='postgres://postgres:postgres@localhost:5432/odtbank?sslmode=disable' \
ADMIN_EMAIL='admin@example.com' ADMIN_PASSWORD='change-this-password' \
go run ./cmd/admin
```

The command prints `admin admin@example.com is ready` when successful. Start or restart the backend with the same `DATABASE_URL`, log in with that identity, and open `http://localhost:3000/admin` to review waiting applications and their passport images. Supplying `ADMIN_EMAIL` and `ADMIN_PASSWORD` to `cmd/server` does not create a PostgreSQL admin; use `cmd/admin` instead.

Create a second administrator with a different email to test adjustment approval. An administrator cannot approve or reject an adjustment they created:

```bash
DATABASE_URL='postgres://postgres:postgres@localhost:5432/odtbank?sslmode=disable' \
ADMIN_EMAIL='checker@example.com' ADMIN_PASSWORD='another-secure-password' \
go run ./cmd/admin
```

### Login troubleshooting

If login returns `invalid email or password`:

- Confirm whether the backend uses memory or PostgreSQL by checking whether `DATABASE_URL` is set.
- In memory mode, stop and restart `cmd/server` with both `ADMIN_EMAIL` and `ADMIN_PASSWORD`. Restarting without them removes the in-memory admin.
- In PostgreSQL mode, apply all migrations and run `cmd/admin` with the same `DATABASE_URL` used by the server.
- Use the exact normalized email and password. Passwords contain 10 to 128 characters.
- Seeded accounts `acc1` and `acc2` have no email or password. Create a customer through `/onboarding`, approve it as an admin, then log in with the submitted customer credentials.

### Event schema

All aggregates share one table:

```sql
CREATE TABLE events (
    aggregate_id  TEXT         NOT NULL,
    sequence      BIGINT       NOT NULL,
    event_type    TEXT         NOT NULL,
    payload       JSONB        NOT NULL,
    occurred_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (aggregate_id, sequence)
);
```

The `(aggregate_id, sequence)` primary key orders each stream and enforces optimistic concurrency. An append at an occupied sequence returns `ErrConcurrencyConflict`.

## API

Responses and non-onboarding request bodies use `application/json`. `/onboarding` and `/login` are public; account and transaction endpoints require the `odtbank_session` cookie. Errors have the form:

```json
{"error":"account not found"}
```

### Onboard a customer

`POST /onboarding`

Creates a customer application with status `waiting_for_approval`. The customer must be at least 18. Requested initial funding may be omitted/zero or at least `10.00`; the account and opening event are created only when an admin approves the application.

The request uses `multipart/form-data` with:

- `payload`: the JSON profile below.
- `passport_image`: a required JPEG, PNG, or WebP image up to 5 MB.

```json
{
  "legal_first_name": "Ada",
  "legal_last_name": "Lovelace",
  "date_of_birth": "1990-12-10",
  "nationality": "GB",
  "email": "ada@example.com",
  "password": "correct-horse-battery-staple",
  "phone": "+66812345678",
  "residential_address": {
    "line1": "1 Computing Lane",
    "line2": "",
    "city": "Bangkok",
    "state_or_province": "",
    "postal_code": "10110",
    "country": "TH"
  },
  "government_document": {
    "type": "passport",
    "number": "P123456",
    "issuing_country": "GB"
  },
  "initial_deposit": 25
}
```

```bash
curl -s http://localhost:8080/onboarding \
  -F 'payload={"legal_first_name":"Ada","legal_last_name":"Lovelace","date_of_birth":"1990-12-10","nationality":"GB","email":"ada@example.com","password":"correct-horse-battery-staple","phone":"+66812345678","residential_address":{"line1":"1 Computing Lane","line2":"","city":"Bangkok","state_or_province":"","postal_code":"10110","country":"TH"},"government_document":{"type":"passport","number":"P123456","issuing_country":"GB"},"initial_deposit":25}' \
  -F 'passport_image=@passport.png;type=image/png'
```

Successful onboarding returns `201`:

```json
{
  "customer_id": "cus_...",
  "kyc_status": "waiting_for_approval"
}
```

The same normalized email or government document cannot be onboarded twice; duplicates return `409`. PostgreSQL requires migrations through `0005_reliable_transfers`. Passport images are available only to authenticated administrators and are never included in account events.

### Log in and out

`POST /login` verifies customer credentials and sets an HTTP-only session cookie:

```bash
curl -s -c cookies.txt http://localhost:8080/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"correct-horse-battery-staple"}'
```

Use `-b cookies.txt` on protected requests. `GET /me` returns the identity role and current onboarding status. Waiting or rejected customers cannot use banking endpoints; rejected customers receive the final review reason. `POST /logout` invalidates the session and clears the cookie.

### Review applications

Admin sessions can list `GET /admin/applications?status=waiting_for_approval`, inspect an application and its `/passport` resource, then use:

```text
POST /admin/applications/{customer_id}/approve
POST /admin/applications/{customer_id}/reject  {"reason":"Passport image is unreadable"}
```

Approval creates and funds the account exactly once. Approval and rejection are terminal; subsequent review attempts return `409`.

The admin dashboard is split into a navigation menu and main content. **Approval** contains sub-menu filters for **Waiting for approval**, **Approved**, and **Rejected** applications. Choose **Query transaction** to inspect any account's balance and grouped transaction history. The same history is available directly from `GET /admin/accounts/{account_id}/events`; customer sessions cannot access this endpoint.

### Adjust balances and reverse transactions

The admin console's **Adjustments** area supports manual credits/debits and full transaction reversals. Every request starts in `waiting_for_approval`; a different administrator must approve or reject it. Approval appends auditable ledger events with the case reference and customer-visible reason. Debits cannot overdraw an account, a transaction can be reversed only once, and reversing a transfer restores the original fee to the source account.

```text
POST /admin/adjustments
GET  /admin/adjustments?status=waiting_for_approval
GET  /admin/adjustments/{adjustment_id}
POST /admin/adjustments/{adjustment_id}/approve
POST /admin/adjustments/{adjustment_id}/reject
```

Manual correction request:

```json
{
  "type": "manual",
  "account_id": "acc1",
  "direction": "credit",
  "amount": 25,
  "reason": "Correction for duplicated service fee",
  "case_reference": "CASE-2026-001"
}
```

Transfer reversal request:

```json
{
  "type": "reversal",
  "original_transfer_id": "trf_...",
  "reason": "Transfer posted to the wrong beneficiary",
  "case_reference": "CASE-2026-002"
}
```

### Transfer

`POST /transfer`

```bash
curl -s http://localhost:8080/transfer \
  -b cookies.txt \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: 550e8400-e29b-41d4-a716-446655440000' \
  -d '{"amount":10,"source_account_id":"acc1","destination_account_id":"acc2"}'
```

```json
{
  "TransferID": "trf_...",
  "Status": "completed",
  "InitialSourceAccount": {"ID":"acc1","Balance":100},
  "FinalSourceAccount": {"ID":"acc1","Balance":90},
  "DestinationAccountID": "acc2",
  "TransferAmount": 10,
  "FeeAmount": 0
}
```

`Idempotency-Key` is required and scoped to the source account. Retrying the same transfer with the same key returns the original receipt without appending more events. Reusing it with different details returns `409`. `GET /transfers/{transfer_id}` returns the initiating customer's durable `pending`, `completed`, or `failed` status.

The default wiring uses `ZeroFeePolicy`; flat fees use integer cents and percentage fees use integer basis points with half-up cent rounding.

### Deposit

`POST /deposit`

```bash
curl -s http://localhost:8080/deposit \
  -b cookies.txt \
  -H 'Content-Type: application/json' \
  -d '{"account_id":"acc1","amount":10}'
```

```json
{
  "InitialAccount": {"ID":"acc1","Balance":100},
  "FinalAccount": {"ID":"acc1","Balance":110},
  "DepositAmount": 10
}
```

### Withdraw

`POST /withdraw`

```bash
curl -s http://localhost:8080/withdraw \
  -b cookies.txt \
  -H 'Content-Type: application/json' \
  -d '{"account_id":"acc1","amount":10}'
```

```json
{
  "InitialAccount": {"ID":"acc1","Balance":100},
  "FinalAccount": {"ID":"acc1","Balance":90},
  "WithdrawalAmount": 10
}
```

A withdrawal may reduce the balance to exactly zero but cannot exceed the available balance.

### Read accounts

`GET /accounts` returns only the authenticated customer's account. `GET /accounts/{id}/events` rejects access to any other account. Both require the session cookie.

```json
{
  "accounts": [
    {"id":"acc1","balance":90,"event_count":2},
    {"id":"acc2","balance":60,"event_count":2}
  ]
}
```

`GET /accounts/{id}/events`. Transfer entries include a shared `transfer_id`, purpose, and counterparty account ID; the web timeline groups the transfer debit and fee together.

```json
{
  "aggregate_id": "acc1",
  "events": [
    {"seq":0,"type":"AccountOpened","amount":100,"occurred_at":"2026-08-23T06:25:51Z"},
    {"seq":1,"type":"MoneyDebited","amount":10,"occurred_at":"2026-08-23T06:25:52Z"}
  ]
}
```

### Error status codes

| Status | Meaning                                                         |
| ------ | --------------------------------------------------------------- |
| `400`  | Malformed JSON or an amount below the operation minimum.        |
| `401`  | Login credentials or the session cookie are invalid.            |
| `403`  | The authenticated customer does not own the requested account.  |
| `404`  | The requested source, destination, or target account is absent. |
| `409`  | A concurrent append occurred or the KYC document already exists. |
| `422`  | The source or withdrawal account has insufficient funds.        |
| `503`  | Transfers are disabled by the configured `TimeService`.         |
| `500`  | An unexpected persistence or server error occurred.             |

## Event model

| Stored event    | Effect during account replay                                  |
| --------------- | ------------------------------------------------------------- |
| `AccountOpened` | Sets the initial balance.                                     |
| `MoneyCredited` | Adds a deposit or destination-side transfer credit.           |
| `MoneyDebited`  | Subtracts a withdrawal, fee, or source-side transfer debit.   |

After a successful transfer, the service also publishes an in-process `TransferCompletedEvent`. This integration event is not stored in the account streams.

## Build and test

Backend checks:

```bash
make build
make vet
make test
```

Run the black-box HTTP scenarios for transfer, deposit, and withdrawal:

```bash
go test ./internal/httpapi -run 'EndToEnd$' -v
```

Dashboard checks:

```bash
cd web
npm run lint
npm run build
```

## Current limitations

- KYC decisions are manual demo decisions; there is no identity provider or sanctions screening.
- Customer KYC is stored without application-level field encryption. Never submit real personal data to this demo.
- Sessions have no rotation, device management, password reset, email verification, or rate limiting.
- Money is represented internally as signed 64-bit minor units for one implicit two-decimal currency; multi-currency is not supported.
- Transfer workflow records are retained indefinitely and there is no pending-transfer reconciliation worker.
- Optimistic concurrency conflicts are returned to clients and are not retried.
- The event bus is in-process and fire-and-forget, with no durable delivery, backpressure, or handler panic recovery.
- Event payloads have no schema versioning or upcasting strategy.
- Account reads replay the complete event stream; snapshots are not implemented.

## License

Licensed under the GNU General Public License v3.0. See [LICENSE](LICENSE).

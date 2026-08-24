# go-odtbank

An event-sourced banking demo built with Go, PostgreSQL, and Next.js. It supports transfers, deposits, withdrawals, account balances, and per-account transaction history.

Account balances are not stored as mutable rows. Each balance is rebuilt by replaying an append-only stream of `AccountOpened`, `MoneyCredited`, and `MoneyDebited` events.

## Features

- Open an individual account through a three-step KYC onboarding journey.
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
        └── WithdrawService
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
internal/service/       Onboarding, transfer, deposit, and withdrawal use cases
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

Restarting the in-memory server resets all events.

### Dashboard

In a second terminal:

```bash
cd web
npm install
npm run dev
```

Open `http://localhost:3000`. The navigation displays one feature at a time: Onboarding, Transfer, Deposit, Withdraw, or Transaction history.

The browser calls `http://localhost:8080` by default. Override it when needed:

```bash
NEXT_PUBLIC_API_URL='https://api.example.com' npm run dev
```

The backend accepts browser requests through its CORS middleware. Set `CORS_ORIGINS` to a comma-separated origin allowlist; when unset, the middleware uses the request origin or `*`.

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
| `make down`         | Stop the containers.                         |

PostgreSQL persists data in the `postgres-data` Docker volume. Use `docker compose down -v` only when you also want to remove that data.

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

Responses and non-onboarding request bodies use `application/json`. Errors have the form:

```json
{"error":"account not found"}
```

### Onboard a customer

`POST /onboarding`

Creates a verified demo customer and account. The customer must be at least 18. Initial funding may be omitted/zero or at least `10.00`.

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
  -F 'payload={"legal_first_name":"Ada","legal_last_name":"Lovelace","date_of_birth":"1990-12-10","nationality":"GB","email":"ada@example.com","phone":"+66812345678","residential_address":{"line1":"1 Computing Lane","line2":"","city":"Bangkok","state_or_province":"","postal_code":"10110","country":"TH"},"government_document":{"type":"passport","number":"P123456","issuing_country":"GB"},"initial_deposit":25}' \
  -F 'passport_image=@passport.png;type=image/png'
```

Successful onboarding returns `201`:

```json
{
  "customer_id": "cus_...",
  "account_id": "acc_...",
  "kyc_status": "verified",
  "balance": 25
}
```

The same normalized government document cannot be onboarded twice; duplicates return `409`. PostgreSQL onboarding requires migration `0002_customers`. The passport image is stored privately in the customer record and is never included in account events or read APIs.

### Transfer

`POST /transfer`

```bash
curl -s http://localhost:8080/transfer \
  -H 'Content-Type: application/json' \
  -d '{"amount":10,"source_account_id":"acc1","destination_account_id":"acc2"}'
```

```json
{
  "InitialSourceAccount": {"ID":"acc1","Balance":100},
  "InitialDestinationAccount": {"ID":"acc2","Balance":50},
  "FinalSourceAccount": {"ID":"acc1","Balance":90},
  "FinalDestinationAccount": {"ID":"acc2","Balance":60},
  "TransferAmount": 10,
  "FeeAmount": 0
}
```

The default wiring uses `ZeroFeePolicy`; flat and percentage fee policies are also available.

### Deposit

`POST /deposit`

```bash
curl -s http://localhost:8080/deposit \
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

`GET /accounts`

```json
{
  "accounts": [
    {"id":"acc1","balance":90,"event_count":2},
    {"id":"acc2","balance":60,"event_count":2}
  ]
}
```

`GET /accounts/{id}/events`

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

- KYC verification is simulated and immediately marked verified; there is no identity provider, document upload, sanctions screening, or manual review.
- Customer KYC is stored without application-level field encryption. Never submit real personal data to this demo.
- There is no authentication or ownership authorization; the dashboard is shared.
- A transfer appends to two account streams without a transaction spanning both appends. A debit can therefore succeed before a destination credit fails.
- Money uses `float64`; production financial software should use integer minor units or a decimal representation.
- Optimistic concurrency conflicts are returned to clients and are not retried.
- The event bus is in-process and fire-and-forget, with no durable delivery, backpressure, or handler panic recovery.
- Event payloads have no schema versioning or upcasting strategy.
- Account reads replay the complete event stream; snapshots are not implemented.

## License

Licensed under the GNU General Public License v3.0. See [LICENSE](LICENSE).

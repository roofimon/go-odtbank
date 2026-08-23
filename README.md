# go-odtbank

A small Go HTTP service that exposes a single money-transfer endpoint. The account ledger is **event-sourced**: account state is derived from an append-only event log, not mutated in place. Two interchangeable store implementations ship — in-memory and Postgres.

## Overview

- **Domain** defines entities and contracts with no external dependencies. `Account` is a stateless struct; current state is folded from events.
- **Service** holds the core use case (transfer money between accounts), orchestrating event appends against an `eventstore.Store`.
- **Event store** persists the append-only log. Two implementations:
  - `eventstore.MemoryStore` — used for tests and zero-setup local runs.
  - `eventstore.PostgresStore` — backed by a single `events` table, gated on the `DATABASE_URL` env var.
- **Repository** is a thin projection over the event store (replays events on demand).
- **Policy** encapsulates fee calculation and service-availability rules.
- **Event bus** publishes a `TransferCompletedEvent` after each successful transfer (the integration event, distinct from the per-account stored events).

The HTTP entry point wires all of these together and serves a single `POST /transfer` route.

## Project Layout

```
.
├── go.mod
├── go.sum
├── docker-compose.yml         # Local Postgres for development
├── Makefile                   # up / down / migrate / run / test / web-dev
├── migrations/                # Plain SQL migrations for the `events` table
│   ├── 0001_init.up.sql
│   └── 0001_init.down.sql
├── web/                       # Next.js (TS) dashboard (see web/ section below)
├── cmd/
│   └── server/
│       └── main.go            # Entry point: wires dependencies, starts HTTP server
└── internal/
    ├── domain/                # Entities + interface contracts (no external deps)
    │   ├── account.go         # Account, TransferReceipt, ReplayAccount, errors
    │   ├── events.go          # Event interface + AccountOpened / MoneyDebited / MoneyCredited
    │   └── interfaces.go      # AccountRepository, FeePolicy, TimeService, TransferService
    ├── service/
    │   ├── transfer_service.go        # Core use case: load → replay → emit events
    │   └── transfer_service_test.go   # Smoke test against in-memory store
    ├── repository/
    │   └── memory_repo.go     # Thin projection over eventstore (replays on read)
    ├── policy/
    │   └── policies.go        # Flat / Zero / Variable fee policies + time service
    ├── eventbus/
    │   └── event_bus.go       # In-process pub/sub for TransferCompletedEvent
    └── eventstore/
        ├── store.go           # Store interface + ErrConcurrencyConflict
        ├── memory_store.go    # In-memory append-only log
        └── postgres_store.go  # Postgres-backed append-only log (pgx/v5)

web/                           # Next.js (TS) browser dashboard
├── components/
│   ├── accounts-table.tsx     # Account list with balances
│   ├── event-log.tsx          # Per-aggregate event stream table
│   └── transfer-form.tsx      # Money-transfer form
└── lib/
    ├── api.ts                 # Typed client for the Go backend
    ├── format.ts              # Money / date formatting helpers
    └── types.ts               # Shared TS types matching the Go wire format
```

## Requirements

- Go **1.27.0** or newer (per `go.mod`).
- Module path: `go-odtbank`.
- For the Postgres path: a running Postgres instance reachable via `DATABASE_URL`. Migrations are applied via Docker (no local CLI needed).

## Dependencies

| Module                                    | Purpose                                              |
| ----------------------------------------- | ---------------------------------------------------- |
| `github.com/gorilla/mux` v1.8.1           | HTTP request routing and method matching.              |
| `github.com/jackc/pgx/v5` v5.x            | Postgres driver (only pulled in for the Postgres path). |

## Build & Test

From the project root:

```bash
go build ./...
go test ./...
```

## Run — In-memory (zero setup)

```bash
go run ./cmd/server
```

If `DATABASE_URL` is **not** set, the server uses the in-memory event store. Two demo accounts are seeded on first startup:

| ID   | Balance |
| ---- | ------- |
| acc1 | 100.0   |
| acc2 | 50.0    |

## Run — Postgres

A `docker-compose.yml` is included for local development with two services:

- **postgres** — `postgres:16-alpine` (database `odtbank`, user/password
  `postgres`/`postgres`) on port **5432**, with a named volume
  (`postgres-data`) so data survives restarts, and a `pg_isready` healthcheck.
- **migrate** — the [`migrate/migrate`](https://hub.docker.com/r/migrate/migrate) image,
  wired to `postgres` by service name so it can reach the database. It's
  grouped under the `tools` profile so it never starts with a plain
  `docker compose up`; you invoke it on demand for one-off migrations.

> No local [`migrate`](https://github.com/golang-migrate/migrate) CLI is
> required — the Docker image handles it.

### Using `docker compose` directly

```bash
# 1. Start the Postgres container in the background.
docker compose up -d postgres

# 2. Wait until it reports healthy.
docker compose ps postgres

# 3. Apply migrations (runs the `migrate/migrate` image once, then exits).
docker compose run --rm migrate up

# 4. Run the server with DATABASE_URL pointing at the container.
DATABASE_URL='postgres://postgres:postgres@localhost:5432/odtbank?sslmode=disable' go run ./cmd/server

# 5. Roll back the most recent migration (optional).
docker compose run --rm migrate down 1

# 6. Stop and remove the containers (data is kept in the volume).
docker compose down

# Or remove the containers *and* the data volume.
docker compose down -v
```

### Using the Makefile

`make` wraps the same steps (see the target table below):

```bash
make up           # docker compose up -d postgres
make migrate      # run the migrate image: up
make migrate-down # run the migrate image: down 1
make run          # run the server with DATABASE_URL set
make down         # docker compose down
```

When `DATABASE_URL` is set, the server constructs `eventstore.PostgresStore` instead of the in-memory one. The choice lives at the wiring layer in `cmd/server/main.go`; the service layer is store-agnostic.

### Useful Make targets

| Target           | What it does                                       |
| ---------------- | -------------------------------------------------- |
| `make up`        | Start the Postgres container.                      |
| `make down`      | Stop and remove the container.                     |
| `make migrate`   | Apply all up migrations via the `migrate` CLI.     |
| `make migrate-down` | Roll back the most recent migration.            |
| `make build`     | `go build ./...`                                   |
| `make vet`       | `go vet ./...`                                     |
| `make test`      | `go test ./...`                                    |
| `make run`       | Run the server with `DATABASE_URL` set.            |

### Schema

The Postgres store uses a single table (see `migrations/0001_init.up.sql`):

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

- `(aggregate_id, sequence)` is the primary key — it gives both fast per-aggregate reads and free optimistic-concurrency on insert.
- The payload column is JSONB; the Go side decodes it back into the right concrete event struct based on `event_type`.

## API

### `POST /transfer`

Transfer funds from one account to another.

**Request body** (`application/json`):

```json
{
  "amount": 10.0,
    "source_account_id": "acc1",
    "destination_account_id": "acc2"
}
```

**Successful response** (`200 OK`, `application/json`):

```json
{
  "InitialSourceAccount":      { "ID": "acc1", "Balance": 100.0 },
  "InitialDestinationAccount": { "ID": "acc2", "Balance": 50.0  },
  "FinalSourceAccount":        { "ID": "acc1", "Balance": 75.0  },
  "FinalDestinationAccount":   { "ID": "acc2", "Balance": 75.0  },
  "TransferAmount":            10.0,
  "FeeAmount":                 0.0
}
```

On error the server returns a JSON body `{"error": "..."}` with a meaningful status code:

| Error                       | Meaning                                                | Status |
| --------------------------- | ------------------------------------------------------ | ------ |
| `ErrInvalidTransferAmount`  | `amount` is below the minimum (1.0).                   | 400    |
| `ErrOutOfService`           | The `TimeService` reports the service is unavailable.  | 503    |
| `ErrAccountNotFound`        | Source or destination account does not exist.          | 404    |
| `ErrConcurrencyConflict`    | Another writer appended to the same aggregate concurrently. | 409 |
| `InsufficientFundsError`    | Source account balance is below `amount` (+ fee).      | 422    |

### `GET /accounts`

List all seeded accounts with their replayed balances.

**Response** (`200 OK`, `application/json`):

```json
{
  "accounts": [
    { "id": "acc1", "balance": 90.0, "event_count": 2 },
    { "id": "acc2", "balance": 60.0, "event_count": 2 }
  ]
}
```

### `GET /accounts/{id}/events`

The full event stream for one aggregate (the event-sourced source of truth).

**Response** (`200 OK`, `application/json`):

```json
{
  "aggregate_id": "acc1",
  "events": [
    { "seq": 0, "type": "AccountOpened", "amount": 100.0, "occurred_at": "2026-08-23T06:25:51Z" },
    { "seq": 1, "type": "MoneyDebited",  "amount": 10.0,  "occurred_at": "2026-08-23T06:25:52Z" }
  ]
}
```

## Web interface

A Next.js (TypeScript) dashboard lives in `web/`. It reads accounts, shows each account's event log, and submits transfers. The backend exposes the endpoints above and permissive CORS so the browser app can reach it directly.

```bash
# Terminal 1 — backend
go run ./cmd/server          # or: make run

# Terminal 2 — frontend (http://localhost:3000)
cd web && npm install && npm run dev
```

The UI calls `http://localhost:8080` by default; set `NEXT_PUBLIC_API_URL` to point elsewhere.

### Example

```bash
curl -s -X POST http://localhost:8080/transfer \
  -H 'Content-Type: application/json' \
  -d '{"amount": 25.0, "source_account_id": "acc1", "destination_account_id": "acc2"}'
```

## Architecture

```
                    ┌────────────────────┐
   HTTP request ──▶ │  cmd/server/main   │ ── wires everything ──┐
                    └────────────────────┘                        │
                                                                 ▼
   ┌────────────┐    ┌─────────────────────┐    ┌──────────────────────────┐
   │  HTTP /    │──▶ │ TransferService     │──▶ │ eventstore.Store         │
   │  gorilla   │    │  (internal/service) │    │  ┌─────────────────────┐ │
   └────────────┘    └──────────┬──────────┘    │  │ MemoryStore (default)│ │
                                │               │  │ PostgresStore (DSN)  │ │
                ┌───────────────┼────────────────┤  └─────────────────────┘ │
                ▼               ▼                ▼                          │
        ┌──────────────┐ ┌──────────────┐ ┌──────────────────────┐           │
        │ FeePolicy    │ │ TimeService  │ │ EventBus callback    │           │
        │ (policy pkg) │ │ (policy pkg) │ │ → TransferCompleted  │           │
        └──────────────┘ └──────────────┘ └──────────────────────┘           │
                                                       │                     │
                                                       ▼                     │
                                              ┌──────────────────────┐       │
                                              │ MemoryAccountRepo    │◀──────┘
                                              │  (replays events)    │
                                              └──────────────────────┘
```

### Dependency direction

All dependencies point inward toward `internal/domain`:

- `domain` defines the interfaces (`AccountRepository`, `FeePolicy`, `TimeService`, `TransferService`) and the entities (`Account`, `Event`).
- `service` depends on `domain` and the `eventstore.Store` interface.
- `eventstore` (memory + Postgres) depends on `domain`.
- `repository`, `policy`, and `eventbus` depend on `domain`.
- `cmd/server` depends on everything to wire the graph.

This keeps the domain free of infrastructure concerns and lets the store be swapped without service-layer changes.

### Transfer flow

1. Validate `amount ≥ 1.0` (otherwise `ErrInvalidTransferAmount`).
2. Check `timeService.IsServiceAvailable(time.Now())` (otherwise `ErrOutOfService`).
3. Load both aggregates' event streams via `eventStore.Load`.
4. Replay each into an `Account` via `domain.ReplayAccount` to derive current state.
5. Compute fee with `FeePolicy.CalculateFee(amount)`; check `source.balance ≥ amount + fee` (otherwise `InsufficientFundsError`).
6. Append `MoneyDebited` events for fee (if any) and amount to the source's stream.
7. Append a `MoneyCredited` event for the amount to the destination's stream.
8. Publish a `TransferCompletedEvent` via the EventBus callback (integration event).
9. Return a `TransferReceipt` with distinct initial/final snapshots.

Optimistic concurrency is enforced by the store: an append with a stale `expectedVersion` returns `ErrConcurrencyConflict`. The service does not currently retry; surfacing the error to the caller is the simplest correct behavior at this stage.

### Event types

Stored events (per-account, used to rebuild state):

| Type             | Trigger                                |
| ---------------- | -------------------------------------- |
| `AccountOpened`  | Seeding an account with initial balance. |
| `MoneyDebited`   | Source-side debit (fee and/or amount). |
| `MoneyCredited`  | Destination-side credit.               |

Integration event (publish-only, not stored):

| Type                       | Trigger                                |
| -------------------------- | -------------------------------------- |
| `TransferCompletedEvent`   | Successful end of a transfer.          |

## Pluggable policies

`internal/policy/policies.go` ships three `FeePolicy` implementations:

| Type                  | Behavior                            |
| --------------------- | ----------------------------------- |
| `ZeroFeePolicy`       | Always returns `0`. (default in `main.go`) |
| `FlatFeePolicy`       | Returns a constant fee.             |
| `VariableFeePolicy`   | Returns `amount * Percentage`.      |

The `TimeService` is provided by `DefaultTimeService`, a simple boolean-flag implementation — useful for tests that need to simulate "service is closed."

## Event bus

`internal/eventbus/event_bus.go` is an in-process pub/sub:

- `Subscribe(handler)` adds a handler under an `RWMutex`-protected slice.
- `Publish(event)` logs the event and invokes each subscribed handler in its own goroutine.

Handlers are fire-and-forget; there is no panic recovery or backpressure.

## Known Limitations

- **No optimistic-concurrency retry** — `ErrConcurrencyConflict` is returned to the caller. A production version would retry on conflict.
- **Goroutine handlers** — event-bus subscribers run via `go handler(event)` with no panic recovery; a panicking subscriber crashes the process.
- **No event upcasting or schema versioning** — old payloads must remain readable forever. Practical for now, but will need a versioning strategy as events evolve.

## Possible Next Steps

- Retry on `ErrConcurrencyConflict` in `TransferService` with bounded attempts.
- Add a panic-recovery wrapper around event-bus subscribers.
- Add a second projection (e.g., per-account transaction history).
- Snapshots for replay performance once event streams grow long.
- Integration tests against a real Postgres (the `migrate` CLI + a `t.Cleanup`-style teardown would make this easy).

## License

Not specified.
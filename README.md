# go-odtbank

A small Go HTTP service that exposes a single money-transfer endpoint, structured around a clean / hexagonal layout with the domain at the center and infrastructure injected via interfaces.

## Overview

`go-odtbank` demonstrates a minimal but realistic layered Go backend:

- **Domain** defines entities and contracts with no external dependencies.
- **Service** holds the core use case (transfer money between accounts).
- **Repository** persists accounts in memory.
- **Policy** encapsulates fee calculation and service-availability rules.
- **Event bus** publishes a `TransferCompletedEvent` after each successful transfer.

The HTTP entry point wires all of these together and serves a single `POST /transfer` route.

## Project Layout

```
.
├── go.mod
├── go.sum
├── cmd/
│   └── server/
│       └── main.go            # Entry point: wires dependencies, starts HTTP server
└── internal/
    ├── domain/                # Entities + interface contracts (no external deps)
    │   ├── account.go         # Account, TransferReceipt, events, errors
    │   └── interfaces.go      # AccountRepository, FeePolicy, TimeService, TransferService
    ├── service/
    │   └── transfer_service.go # TransferService — the core use case
    ├── repository/
    │   └── memory_repo.go     # In-memory AccountRepository (mutex-protected map)
    ├── policy/
    │   └── policies.go        # Flat / Zero / Variable fee policies + time service
    └── eventbus/
        └── event_bus.go       # In-process pub/sub for TransferCompletedEvent
```

## Requirements

- Go **1.27.0** or newer (per `go.mod`).
- Module path: `go-odtbank`.

## Dependencies

| Module                       | Purpose                        |
| ---------------------------- | ------------------------------ |
| `github.com/gorilla/mux` v1.8.1 | HTTP request routing and method matching. |

## Build & Run

From the project root:

```bash
# Build all packages
go build ./...

# Run the server
go run ./cmd/server
```

The server listens on `:8080`. On startup it prints:

```
Server starting on :8080...
```

Two accounts are seeded for convenience:

| ID   | Balance |
| ---- | ------- |
| acc1 | 100.0   |
| acc2 | 50.0    |

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
  "InitialSourceAccount":      { ... },
  "InitialDestinationAccount": { ... },
  "FinalSourceAccount":        { ... },
  "FinalDestinationAccount":   { ... },
  "TransferAmount":            10.0,
  "FeeAmount":                 0.0
}
```

On error the server currently returns `500 Internal Server Error` with the error message. The service distinguishes:

| Error                       | Meaning                                                |
| --------------------------- | ------------------------------------------------------ |
| `ErrInvalidTransferAmount`  | `amount` is below the minimum (1.0).                   |
| `ErrOutOfService`           | The `TimeService` reports the service is unavailable.  |
| `ErrAccountNotFound`        | Source or destination account does not exist.          |
| `InsufficientFundsError`    | Source account balance is below `amount` (+ fee).      |

> Note: all of the above currently surface as `500`. Mapping them to appropriate `4xx` status codes is a sensible next step.

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
   │  HTTP /    │──▶ │ TransferService     │──▶ │ AccountRepository        │
   │  gorilla   │    │  (internal/service) │    │  (in-memory, mutex map)  │
   └────────────┘    └──────────┬──────────┘    └──────────────────────────┘
                                │
                ┌───────────────┼────────────────┐
                ▼               ▼                ▼
        ┌──────────────┐ ┌──────────────┐ ┌──────────────────────┐
        │ FeePolicy    │ │ TimeService  │ │ EventBus callback    │
        │ (policy pkg) │ │ (policy pkg) │ │ → TransferCompleted  │
        └──────────────┘ └──────────────┘ └──────────────────────┘
```

### Dependency direction

All dependencies point inward toward `internal/domain`:

- `domain` defines the interfaces (`AccountRepository`, `FeePolicy`, `TimeService`, `TransferService`) and the entity (`Account`).
- `service` depends on `domain`.
- `repository`, `policy`, and `eventbus` depend on `domain`.
- `cmd/server` depends on everything to wire the graph.

This keeps the domain free of infrastructure concerns and trivially testable.

### Transfer flow

1. Validate `amount ≥ 1.0` (otherwise `ErrInvalidTransferAmount`).
2. Check `timeService.IsServiceAvailable(time.Now())` (otherwise `ErrOutOfService`).
3. Load source and destination accounts via `AccountRepository.FindByID`.
4. Compute fee with `FeePolicy.CalculateFee(amount)`; debit the fee from the source if non-zero.
5. `Debit(amount)` from source — fails with `InsufficientFundsError` if balance is too low.
6. `Credit(amount)` to destination.
7. Persist both balances via `UpdateBalance`.
8. Publish a `TransferCompletedEvent` through the event-bus callback.
9. Return a `TransferReceipt`.

### Pluggable policies

`internal/policy/policies.go` ships three `FeePolicy` implementations:

| Type                  | Behavior                            |
| --------------------- | ----------------------------------- |
| `ZeroFeePolicy`       | Always returns `0`. (default in `main.go`) |
| `FlatFeePolicy`       | Returns a constant fee.             |
| `VariableFeePolicy`   | Returns `amount * Percentage`.      |

The `TimeService` is provided by `DefaultTimeService`, a simple boolean-flag implementation — useful for tests that need to simulate "service is closed."

### Event bus

`internal/eventbus/event_bus.go` is an in-process pub/sub:

- `Subscribe(handler)` adds a handler under an `RWMutex`-protected slice.
- `Publish(event)` logs the event and invokes each subscribed handler in its own goroutine.

Handlers are fire-and-forget; there is no panic recovery or backpressure.

## Known Limitations

These are honest observations from a careful read of the code; nothing here has been changed.

- **Race on `Account.Balance`**: `Debit` and `Credit` mutate the struct with no lock. The repository's `RWMutex` only protects the map, not the `*Account` pointers it stores. Concurrent transfers to the same account are data races.
- **Persistence errors are swallowed**: `UpdateBalance` return values are discarded (`_ =`). A failed write is invisible to the caller.
- **Coarse HTTP error mapping**: every service error maps to `500`. `ErrAccountNotFound` should be `404`, `ErrInvalidTransferAmount` `400`, `InsufficientFundsError` `422`, etc.
- **Receipt snapshot is shallow**: `InitialSourceAccount` and `FinalSourceAccount` point to the same mutated `*Account`; the "before" state is effectively the post-debit state.
- **Goroutine handlers**: event-bus subscribers run via `go handler(event)` with no panic recovery — a panicking subscriber crashes the process.
- **In-memory state**: accounts are lost when the process restarts. There is no persistence layer.

## Possible Next Steps

- Fix the data race on `Account` (e.g., per-account mutex, or store balances atomically).
- Map domain errors to proper HTTP status codes.
- Snapshot account balances into `TransferReceipt` instead of sharing pointers.
- Add a panic-recovery wrapper around event-bus subscribers.
- Introduce a persistent repository (e.g., PostgreSQL or SQLite).
- Add unit tests for `TransferService` against an in-memory fake repo and a fake `FeePolicy` / `TimeService`.
- Add integration tests using `httptest`.

## License

Not specified.

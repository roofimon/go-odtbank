package eventstore_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
)

func TestPostgresIntegrationEventOutbox(t *testing.T) {
	if os.Getenv("POSTGRES_INTEGRATION") != "1" {
		t.Skip("set POSTGRES_INTEGRATION=1 to run against PostgreSQL")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/odtbank?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := eventstore.NewPostgresStore(pool)

	transferID, source, destination := "trf_outbox", "acc_outbox_source", "acc_outbox_destination"
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM integration_events WHERE transfer_id=$1`, transferID)
		_, _ = pool.Exec(ctx, `DELETE FROM ledger_postings WHERE transfer_id=$1`, transferID)
		_, _ = pool.Exec(ctx, `DELETE FROM compliance_decisions WHERE transfer_id=$1`, transferID)
		_, _ = pool.Exec(ctx, `DELETE FROM account_reservations WHERE transfer_id=$1`, transferID)
		_, _ = pool.Exec(ctx, `DELETE FROM transfers WHERE id=$1`, transferID)
		_, _ = pool.Exec(ctx, `DELETE FROM events WHERE aggregate_id IN ($1,$2)`, source, destination)
	}
	cleanup()
	t.Cleanup(cleanup)

	now := time.Now().UTC()
	if err = store.Append(domain.AccountOpened{Aggregate: source, Type: "AccountOpened", ID: source, InitialBalance: 10000, Occurred: now}, 0); err != nil {
		t.Fatal(err)
	}
	if err = store.Append(domain.AccountOpened{Aggregate: destination, Type: "AccountOpened", ID: destination, InitialBalance: 1000, Occurred: now}, 0); err != nil {
		t.Fatal(err)
	}
	r := domain.TransferRecord{ID: transferID, SourceAccountID: source, DestinationAccountID: destination, IdempotencyKey: "outbox", Amount: 2000, Fee: 100, Status: domain.TransferPending, CurrentStep: domain.SagaCreated, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}
	if _, _, err = store.CreateTransfer(r); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ReserveTransferFunds(r, now); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordComplianceDecision(r.ID, "approved", now); err != nil {
		t.Fatal(err)
	}
	// After the ledger post, exactly one scheduled outbox row must exist for this
	// transfer, committed with the ledger posting in the same transaction.
	if _, err = store.PostTransferLedger(r, now); err != nil {
		t.Fatal(err)
	}

	mine := func() []domain.IntegrationEvent {
		all, err := store.ListIntegrationEvents("", 0)
		if err != nil {
			t.Fatal(err)
		}
		var out []domain.IntegrationEvent
		for _, r := range all {
			if r.TransferID == transferID {
				out = append(out, r)
			}
		}
		return out
	}
	if got := mine(); len(got) != 1 {
		t.Fatalf("outbox rows for transfer=%d, want 1", len(got))
	}
	row := mine()[0]
	if row.Status != domain.IntegrationEventScheduled {
		t.Fatalf("status=%q, want scheduled", row.Status)
	}
	var tc domain.TransferCompletedEvent
	if err = json.Unmarshal(row.Payload, &tc); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if tc.Amount != 2000 || tc.SourceAccountID != source || tc.DestinationAccountID != destination {
		t.Fatalf("decoded event=%+v", tc)
	}

	// The row can be marked published and a second mark is a no-op.
	if ok, err := store.MarkIntegrationEventPublished(row.ID, now); err != nil || !ok {
		t.Fatalf("first mark ok=%v err=%v", ok, err)
	}
	if ok, err := store.MarkIntegrationEventPublished(row.ID, now); err != nil || ok {
		t.Fatalf("second mark ok=%v, want false", ok)
	}

	// The same transfer's event cannot be enqueued twice.
	if got := mine(); len(got) != 1 || got[0].Status != domain.IntegrationEventPublished {
		t.Fatalf("after publish rows=%+v, want 1 published", got)
	}
	// A duplicate enqueue for the same transfer is a no-op (unique index).
	_ = store.AppendIntegrationEvent(domain.IntegrationEvent{TransferID: transferID, EventType: "TransferCompleted", Payload: []byte("{}"), Status: domain.IntegrationEventScheduled, NextAttemptAt: now, CreatedAt: now})
	if got := mine(); len(got) != 1 {
		t.Fatalf("idempotent enqueue produced %d rows, want 1", len(got))
	}
}

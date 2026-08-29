package eventstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
)

func TestPostgresTransferSagaIntegration(t *testing.T) {
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
	transferID, source, destination := "trf_saga_integration", "acc_saga_source", "acc_saga_destination"
	cleanup := func() {
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
	r := domain.TransferRecord{ID: transferID, SourceAccountID: source, DestinationAccountID: destination, IdempotencyKey: "integration", Amount: 2000, Fee: 100, Status: domain.TransferPending, CurrentStep: domain.SagaCreated, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}
	if _, _, err = store.CreateTransfer(r); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ReserveTransferFunds(r, now); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordComplianceDecision(r.ID, "approved", now); err != nil {
		t.Fatal(err)
	}
	if _, err = store.PostTransferLedger(r, now); err != nil {
		t.Fatal(err)
	}
	if err = store.CaptureTransferReservation(r, now); err != nil {
		t.Fatal(err)
	}
	sourceEvents, _ := store.Load(source)
	destinationEvents, _ := store.Load(destination)
	a := domain.ReplayAccount(source, sourceEvents)
	b := domain.ReplayAccount(destination, destinationEvents)
	if a.Balance != 7900 || a.ReservedBalance != 0 || b.Balance != 3000 {
		t.Fatalf("source=%+v destination=%+v", a, b)
	}
}

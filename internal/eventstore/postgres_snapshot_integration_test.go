package eventstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/repository"
)

func TestPostgresSnapshotMatchesBaseline(t *testing.T) {
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

	const account = "acc_snapshot_baseline"
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM account_snapshots WHERE aggregate_id=$1`, account)
		_, _ = pool.Exec(ctx, `DELETE FROM events WHERE aggregate_id=$1`, account)
	}
	cleanup()
	t.Cleanup(cleanup)

	occ := time.Now().UTC()
	const total = 30
	if err := store.Append(domain.AccountOpened{Aggregate: account, Type: "AccountOpened", ID: account, InitialBalance: domain.Money(100000), Occurred: occ}, 0); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < total; i++ {
		if err := store.Append(domain.MoneyCredited{Aggregate: account, Type: "MoneyCredited", Seq: i, Occurred: occ.Add(time.Duration(i)), ID: account, Amount: domain.Money(100)}, i); err != nil {
			t.Fatal(err)
		}
	}

	events, err := store.Load(account)
	if err != nil {
		t.Fatal(err)
	}
	baseline := domain.ReplayAccount(account, events)

	repo := repository.NewMemoryAccountRepository(store, 10)
	got, err := repo.FindByID(account)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Balance != baseline.Balance || got.AvailableBalance != baseline.AvailableBalance {
		t.Fatalf("snapshot read got=%+v baseline=%+v", got, baseline)
	}

	snap, err := store.LoadSnapshot(account)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if snap.AsOfSequence != total-1 {
		t.Fatalf("snapshot as_of_sequence=%d want=%d", snap.AsOfSequence, total-1)
	}
}

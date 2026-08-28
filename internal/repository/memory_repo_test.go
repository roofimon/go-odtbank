package repository_test

import (
	"testing"
	"time"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/repository"
)

// TestSnapshotAwareRepositoryMatchesBaseline builds a long stream, reads through
// the snapshot-aware repository with a low threshold, and asserts the derived
// account equals a full replay of the same stream.
func TestSnapshotAwareRepositoryMatchesBaseline(t *testing.T) {
	store := eventstore.NewMemoryStore()
	occ := time.Now().UTC()

	const opens = 1
	const total = 25
	for i := 0; i < opens; i++ {
		if err := store.Append(domain.AccountOpened{Aggregate: "acc1", Type: "AccountOpened", Seq: i, Occurred: occ, ID: "acc1", InitialBalance: domain.Money(100000)}, i); err != nil {
			t.Fatal(err)
		}
	}
	for i := opens; i < total; i++ {
		if err := store.Append(domain.MoneyCredited{Aggregate: "acc1", Type: "MoneyCredited", Seq: i, Occurred: occ.Add(time.Duration(i)), ID: "acc1", Amount: domain.Money(100)}, i); err != nil {
			t.Fatal(err)
		}
	}

	baseline := domain.ReplayAccount("acc1", mustLoad(t, store, "acc1"))

	repo := repository.NewMemoryAccountRepository(store, 10)
	got, err := repo.FindByID("acc1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Balance != baseline.Balance || got.AvailableBalance != baseline.AvailableBalance {
		t.Fatalf("snapshot read got=%+v baseline=%+v", got, baseline)
	}

	// A snapshot must have been materialized because the stream crossed the
	// threshold, and it must reflect the full stream's end.
	snap, err := store.LoadSnapshot("acc1")
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if snap.AsOfSequence != total-1 {
		t.Fatalf("snapshot as_of_sequence=%d want=%d", snap.AsOfSequence, total-1)
	}

	// A second read after the snapshot exists must still equal the baseline,
	// proving the tail fold recovers the full state.
	again, err := repo.FindByID("acc1")
	if err != nil {
		t.Fatalf("FindByID (second): %v", err)
	}
	if again.Balance != baseline.Balance || again.AvailableBalance != baseline.AvailableBalance {
		t.Fatalf("post-snapshot read got=%+v baseline=%+v", again, baseline)
	}
}

func mustLoad(t *testing.T, store *eventstore.MemoryStore, id string) []domain.Event {
	t.Helper()
	events, err := store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

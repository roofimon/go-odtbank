package repository

import (
	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
)

// MemoryAccountRepository is a thin projection over the event store.
// It reads an aggregate by replaying its events, returning the current state
// derived from the log rather than from mutable in-memory rows.
//
// When SnapshotThreshold is positive, a long stream is read from a materialized
// snapshot plus its tail, and a fresh snapshot is written lazily once the stream
// reaches the threshold. A snapshot write failure never fails the read.
type MemoryAccountRepository struct {
	eventStore       eventstore.Store
	snapshotThreshold int
}

func NewMemoryAccountRepository(store eventstore.Store, snapshotThreshold int) *MemoryAccountRepository {
	return &MemoryAccountRepository{eventStore: store, snapshotThreshold: snapshotThreshold}
}

func (r *MemoryAccountRepository) FindByID(id string) (*domain.Account, error) {
	events, err := r.eventStore.Load(id)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, domain.ErrAccountNotFound
	}

	account := domain.ReplayAccountFrom(id, r.snapshot(id), events)
	r.maybeSnapshot(id, events, account)
	return account, nil
}

// snapshot returns the latest stored snapshot, or nil when none exists or the
// store reports a read error (a degraded read still replays the full stream).
func (r *MemoryAccountRepository) snapshot(id string) *domain.AccountSnapshot {
	snap, err := r.eventStore.LoadSnapshot(id)
	if err != nil {
		return nil
	}
	return snap
}

// maybeSnapshot lazily materializes a snapshot once a stream grows past the
// threshold. A write error is ignored so reads stay resilient.
func (r *MemoryAccountRepository) maybeSnapshot(id string, events []domain.Event, account *domain.Account) {
	if r.snapshotThreshold <= 0 || len(events) < r.snapshotThreshold {
		return
	}
	_ = r.eventStore.SaveSnapshot(domain.AccountSnapshot{
		AggregateID:      id,
		Balance:          account.Balance,
		ReservedBalance:  account.ReservedBalance,
		AvailableBalance: account.AvailableBalance,
		AsOfSequence:     events[len(events)-1].Version(),
		OccurredAt:       events[len(events)-1].OccurredAt(),
	})
}

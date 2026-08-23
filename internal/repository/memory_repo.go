package repository

import (
	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
)

// MemoryAccountRepository is a thin projection over the event store.
// It reads an aggregate by replaying its events, returning the current state
// derived from the log rather than from mutable in-memory rows.
type MemoryAccountRepository struct {
	eventStore eventstore.Store
}

func NewMemoryAccountRepository(store eventstore.Store) *MemoryAccountRepository {
	return &MemoryAccountRepository{eventStore: store}
}

func (r *MemoryAccountRepository) FindByID(id string) (*domain.Account, error) {
	events, err := r.eventStore.Load(id)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, domain.ErrAccountNotFound
	}
	return domain.ReplayAccount(id, events), nil
}

package eventstore

import (
	"sync"

	"go-odtbank/internal/domain"
)

// MemoryStore is an in-memory append-only event log, keyed by aggregate ID.
// It is the prototype implementation of Store; the Postgres implementation
// will satisfy the same interface.
type MemoryStore struct {
	mu      sync.RWMutex
	streams map[string][]domain.Event
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		streams: make(map[string][]domain.Event),
	}
}

// Append stores the event at the next sequence position for its aggregate,
// provided expectedVersion matches the current stream length.
func (m *MemoryStore) Append(event domain.Event, expectedVersion int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := event.AggregateID()
	stream := m.streams[id]
	if len(stream) != expectedVersion {
		return ErrConcurrencyConflict
	}

	m.streams[id] = append(stream, event)
	return nil
}

// Load returns a copy of the aggregate's event stream.
func (m *MemoryStore) Load(aggregateID string) ([]domain.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	src := m.streams[aggregateID]
	out := make([]domain.Event, len(src))
	copy(out, src)
	return out, nil
}
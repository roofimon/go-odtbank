package eventstore

import (
	"sort"
	"sync"
	"time"

	"go-odtbank/internal/domain"
)

// MemoryStore is an in-memory append-only event log, keyed by aggregate ID.
// It is the prototype implementation of Store; the Postgres implementation
// will satisfy the same interface.
type MemoryStore struct {
	mu        sync.RWMutex
	streams   map[string][]domain.Event
	customers map[string]domain.Customer
	sessions  map[string]domain.Session
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		streams:   make(map[string][]domain.Event),
		customers: make(map[string]domain.Customer),
		sessions:  make(map[string]domain.Session),
	}
}

func (m *MemoryStore) CreateCustomerAccount(customer domain.Customer, opened domain.AccountOpened) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.customers {
		if existing.Email == customer.Email {
			return domain.ErrCustomerAlreadyExists
		}
		if existing.GovernmentDocument.Type == customer.GovernmentDocument.Type &&
			existing.GovernmentDocument.Number == customer.GovernmentDocument.Number &&
			existing.GovernmentDocument.IssuingCountry == customer.GovernmentDocument.IssuingCountry {
			return domain.ErrCustomerAlreadyExists
		}
	}
	if _, exists := m.customers[customer.ID]; exists {
		return domain.ErrCustomerAlreadyExists
	}
	if len(m.streams[customer.AccountID]) != 0 {
		return ErrConcurrencyConflict
	}
	m.customers[customer.ID] = customer
	m.streams[customer.AccountID] = []domain.Event{opened}
	return nil
}

func (m *MemoryStore) FindCustomerByEmail(email string) (*domain.Customer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, customer := range m.customers {
		if customer.Email == email {
			copy := customer
			return &copy, nil
		}
	}
	return nil, domain.ErrInvalidCredentials
}

func (m *MemoryStore) CreateSession(session domain.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[session.TokenHash] = session
	return nil
}

func (m *MemoryStore) FindSession(tokenHash string, now time.Time) (*domain.Principal, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[tokenHash]
	if !ok || !session.ExpiresAt.After(now) {
		return nil, domain.ErrUnauthorized
	}
	customer, ok := m.customers[session.CustomerID]
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	return &domain.Principal{CustomerID: customer.ID, AccountID: customer.AccountID, Email: customer.Email}, nil
}

func (m *MemoryStore) DeleteSession(tokenHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, tokenHash)
	return nil
}

var _ domain.OnboardingStore = (*MemoryStore)(nil)
var _ domain.AuthStore = (*MemoryStore)(nil)

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

// ListAggregates returns the IDs of all aggregates that have at least one
// event, sorted ascending.
func (m *MemoryStore) ListAggregates() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]string, 0, len(m.streams))
	for id := range m.streams {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

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
	admins    map[string]domain.Admin
	sessions  map[string]domain.Session
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		streams:   make(map[string][]domain.Event),
		customers: make(map[string]domain.Customer),
		admins:    make(map[string]domain.Admin),
		sessions:  make(map[string]domain.Session),
	}
}

func (m *MemoryStore) CreateCustomerApplication(customer domain.Customer) error {
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
	m.customers[customer.ID] = customer
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

func (m *MemoryStore) FindAdminByEmail(email string) (*domain.Admin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, admin := range m.admins {
		if admin.Email == email {
			copy := admin
			return &copy, nil
		}
	}
	return nil, domain.ErrInvalidCredentials
}

func (m *MemoryStore) UpsertAdmin(admin domain.Admin) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, existing := range m.admins {
		if existing.Email == admin.Email {
			admin.ID = id
			m.admins[id] = admin
			return nil
		}
	}
	m.admins[admin.ID] = admin
	return nil
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
	if session.AdminID != "" {
		admin, ok := m.admins[session.AdminID]
		if !ok {
			return nil, domain.ErrUnauthorized
		}
		return &domain.Principal{AdminID: admin.ID, Email: admin.Email, Role: "admin"}, nil
	}
	customer, ok := m.customers[session.CustomerID]
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	return &domain.Principal{CustomerID: customer.ID, AccountID: customer.AccountID, Email: customer.Email, Role: "customer", KYCStatus: customer.KYCStatus, RejectionReason: customer.RejectionReason}, nil
}

func (m *MemoryStore) DeleteSession(tokenHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, tokenHash)
	return nil
}

var _ domain.OnboardingStore = (*MemoryStore)(nil)
var _ domain.AuthStore = (*MemoryStore)(nil)
var _ domain.ReviewStore = (*MemoryStore)(nil)

func summary(customer domain.Customer) domain.ApplicationSummary {
	return domain.ApplicationSummary{CustomerID: customer.ID, LegalFirstName: customer.LegalFirstName, LegalLastName: customer.LegalLastName, Email: customer.Email, KYCStatus: customer.KYCStatus, RequestedDeposit: customer.RequestedDeposit, CreatedAt: customer.CreatedAt, ReviewedAt: customer.ReviewedAt, RejectionReason: customer.RejectionReason}
}

func (m *MemoryStore) ListApplications(status string) ([]domain.ApplicationSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []domain.ApplicationSummary{}
	for _, c := range m.customers {
		if c.KYCStatus == status {
			out = append(out, summary(c))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (m *MemoryStore) GetApplication(id string) (*domain.ApplicationDetail, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.customers[id]
	if !ok {
		return nil, domain.ErrApplicationNotFound
	}
	return &domain.ApplicationDetail{ApplicationSummary: summary(c), DateOfBirth: c.DateOfBirth, Nationality: c.Nationality, Phone: c.Phone, ResidentialAddress: c.ResidentialAddress, GovernmentDocument: c.GovernmentDocument, PassportImageMIME: c.PassportImageMIME}, nil
}
func (m *MemoryStore) GetPassportImage(id string) ([]byte, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.customers[id]
	if !ok {
		return nil, "", domain.ErrApplicationNotFound
	}
	return append([]byte(nil), c.PassportImage...), c.PassportImageMIME, nil
}
func (m *MemoryStore) ApproveApplication(id, adminID, accountID string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.customers[id]
	if !ok {
		return domain.ErrApplicationNotFound
	}
	if c.KYCStatus != domain.KYCWaiting {
		return domain.ErrApplicationReviewed
	}
	c.KYCStatus = domain.KYCApproved
	c.AccountID = accountID
	c.ReviewedBy = adminID
	c.ReviewedAt = &at
	m.customers[id] = c
	m.streams[accountID] = []domain.Event{domain.AccountOpened{Aggregate: accountID, Type: "AccountOpened", Seq: 0, Occurred: at, ID: accountID, InitialBalance: c.RequestedDeposit}}
	return nil
}
func (m *MemoryStore) RejectApplication(id, adminID, reason string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.customers[id]
	if !ok {
		return domain.ErrApplicationNotFound
	}
	if c.KYCStatus != domain.KYCWaiting {
		return domain.ErrApplicationReviewed
	}
	c.KYCStatus = domain.KYCRejected
	c.ReviewedBy = adminID
	c.ReviewedAt = &at
	c.RejectionReason = reason
	m.customers[id] = c
	return nil
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

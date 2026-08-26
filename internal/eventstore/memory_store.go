package eventstore

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"go-odtbank/internal/domain"
)

// MemoryStore is an in-memory append-only event log, keyed by aggregate ID.
// It is the prototype implementation of Store; the Postgres implementation
// will satisfy the same interface.
type MemoryStore struct {
	mu          sync.RWMutex
	streams     map[string][]domain.Event
	customers   map[string]domain.Customer
	admins      map[string]domain.Admin
	sessions    map[string]domain.Session
	transfers   map[string]domain.TransferRecord
	adjustments map[string]domain.AdjustmentRequest
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		streams:     make(map[string][]domain.Event),
		customers:   make(map[string]domain.Customer),
		admins:      make(map[string]domain.Admin),
		sessions:    make(map[string]domain.Session),
		transfers:   make(map[string]domain.TransferRecord),
		adjustments: make(map[string]domain.AdjustmentRequest),
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
var _ domain.AtomicTransferStore = (*MemoryStore)(nil)
var _ domain.AdjustmentStore = (*MemoryStore)(nil)

func (m *MemoryStore) CreateAdjustment(r domain.AdjustmentRequest) (*domain.AdjustmentRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.Type == domain.AdjustmentManual {
		if len(m.streams[r.AccountID]) == 0 {
			return nil, domain.ErrAccountNotFound
		}
	} else if r.OriginalTransferID != "" {
		t, ok := m.transfers[r.OriginalTransferID]
		if !ok || t.Status != domain.TransferCompleted {
			return nil, domain.ErrTransferNotFound
		}
		for _, a := range m.adjustments {
			if a.OriginalTransferID == r.OriginalTransferID && a.Status != domain.AdjustmentRejected {
				return nil, domain.ErrAlreadyReversed
			}
		}
		r.AccountID = t.SourceAccountID
		r.CounterpartyAccountID = t.DestinationAccountID
		r.Amount = t.Amount
		r.Fee = t.Fee
		r.Direction = "reversal"
	} else {
		events := m.streams[r.OriginalAccountID]
		if r.OriginalEventSequence == nil || *r.OriginalEventSequence < 0 || *r.OriginalEventSequence >= len(events) {
			return nil, domain.ErrInvalidAdjustment
		}
		for _, a := range m.adjustments {
			if a.OriginalAccountID == r.OriginalAccountID && a.OriginalEventSequence != nil && *a.OriginalEventSequence == *r.OriginalEventSequence && a.Status != domain.AdjustmentRejected {
				return nil, domain.ErrAlreadyReversed
			}
		}
		switch e := events[*r.OriginalEventSequence].(type) {
		case domain.MoneyCredited:
			if e.TransferID != "" || e.Purpose == "fee" || e.AdjustmentID != "" {
				return nil, domain.ErrInvalidAdjustment
			}
			r.AccountID = r.OriginalAccountID
			r.Amount = e.Amount
			r.Direction = "debit"
		case domain.MoneyDebited:
			if e.TransferID != "" || e.Purpose == "fee" || e.AdjustmentID != "" {
				return nil, domain.ErrInvalidAdjustment
			}
			r.AccountID = r.OriginalAccountID
			r.Amount = e.Amount
			r.Direction = "credit"
		default:
			return nil, domain.ErrInvalidAdjustment
		}
	}
	m.adjustments[r.ID] = r
	copy := r
	return &copy, nil
}
func (m *MemoryStore) ListAdjustments(status string) ([]domain.AdjustmentRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []domain.AdjustmentRequest{}
	for _, r := range m.adjustments {
		if r.Status == status {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (m *MemoryStore) GetAdjustment(id string) (*domain.AdjustmentRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.adjustments[id]
	if !ok {
		return nil, domain.ErrAdjustmentNotFound
	}
	return &r, nil
}
func (m *MemoryStore) ApproveAdjustment(id, adminID string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.adjustments[id]
	if !ok {
		return domain.ErrAdjustmentNotFound
	}
	if r.Status != domain.AdjustmentWaiting {
		return domain.ErrAdjustmentReviewed
	}
	if r.CreatedBy == adminID {
		return domain.ErrSelfApproval
	}
	ref := r.OriginalTransferID
	if ref == "" && r.OriginalEventSequence != nil {
		ref = r.OriginalAccountID + ":" + fmt.Sprint(*r.OriginalEventSequence)
	}
	appendCredit := func(account string, amount domain.Money, counterparty string) {
		events := m.streams[account]
		m.streams[account] = append(events, domain.MoneyCredited{Aggregate: account, Type: "MoneyCredited", Seq: len(events), Occurred: at, ID: account, Amount: amount, Purpose: map[bool]string{true: "reversal", false: "adjustment"}[r.Type == domain.AdjustmentReversal], CounterpartyAccountID: counterparty, AdjustmentID: r.ID, AdjustmentReason: r.Reason, CaseReference: r.CaseReference, OriginalReference: ref})
	}
	appendDebit := func(account string, amount domain.Money, counterparty string) error {
		events := m.streams[account]
		if domain.ReplayAccount(account, events).Balance < amount {
			return domain.NewInsufficientFundsError(domain.ReplayAccount(account, events), amount)
		}
		m.streams[account] = append(events, domain.MoneyDebited{Aggregate: account, Type: "MoneyDebited", Seq: len(events), Occurred: at, ID: account, Amount: amount, Purpose: map[bool]string{true: "reversal", false: "adjustment"}[r.Type == domain.AdjustmentReversal], CounterpartyAccountID: counterparty, AdjustmentID: r.ID, AdjustmentReason: r.Reason, CaseReference: r.CaseReference, OriginalReference: ref})
		return nil
	}
	if r.OriginalTransferID != "" {
		if err := appendDebit(r.CounterpartyAccountID, r.Amount, r.AccountID); err != nil {
			return err
		}
		appendCredit(r.AccountID, r.Amount+r.Fee, r.CounterpartyAccountID)
	} else if r.Direction == "credit" {
		appendCredit(r.AccountID, r.Amount, "")
	} else {
		if err := appendDebit(r.AccountID, r.Amount, ""); err != nil {
			return err
		}
	}
	r.Status = domain.AdjustmentApproved
	r.ReviewedBy = adminID
	r.ReviewedAt = &at
	m.adjustments[id] = r
	return nil
}
func (m *MemoryStore) RejectAdjustment(id, adminID, reason string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.adjustments[id]
	if !ok {
		return domain.ErrAdjustmentNotFound
	}
	if r.Status != domain.AdjustmentWaiting {
		return domain.ErrAdjustmentReviewed
	}
	if r.CreatedBy == adminID {
		return domain.ErrSelfApproval
	}
	r.Status = domain.AdjustmentRejected
	r.ReviewedBy = adminID
	r.RejectionReason = reason
	r.ReviewedAt = &at
	m.adjustments[id] = r
	return nil
}

func (m *MemoryStore) ExecuteTransfer(record domain.TransferRecord) (*domain.TransferRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.transfers {
		if existing.SourceAccountID == record.SourceAccountID && existing.IdempotencyKey == record.IdempotencyKey {
			if existing.Amount != record.Amount || existing.DestinationAccountID != record.DestinationAccountID {
				return nil, false, domain.ErrIdempotencyConflict
			}
			copy := existing
			return &copy, false, nil
		}
	}
	srcEvents, dstEvents := m.streams[record.SourceAccountID], m.streams[record.DestinationAccountID]
	if len(srcEvents) == 0 || len(dstEvents) == 0 {
		record.Status = domain.TransferFailed
		record.FailureCode = "account_not_found"
		m.transfers[record.ID] = record
		return &record, true, nil
	}
	src := domain.ReplayAccount(record.SourceAccountID, srcEvents)
	record.InitialSourceBalance = src.Balance
	if src.Balance < record.Amount+record.Fee {
		record.Status = domain.TransferFailed
		record.FailureCode = "insufficient_funds"
		m.transfers[record.ID] = record
		return &record, true, nil
	}
	now := time.Now().UTC()
	if record.Fee > 0 {
		e := domain.MoneyDebited{Aggregate: record.SourceAccountID, Type: "MoneyDebited", Seq: len(srcEvents), Occurred: now, ID: record.SourceAccountID, Amount: record.Fee, TransferID: record.ID, Purpose: "fee", CounterpartyAccountID: record.DestinationAccountID}
		srcEvents = append(srcEvents, e)
	}
	debit := domain.MoneyDebited{Aggregate: record.SourceAccountID, Type: "MoneyDebited", Seq: len(srcEvents), Occurred: now, ID: record.SourceAccountID, Amount: record.Amount, TransferID: record.ID, Purpose: "transfer", CounterpartyAccountID: record.DestinationAccountID}
	credit := domain.MoneyCredited{Aggregate: record.DestinationAccountID, Type: "MoneyCredited", Seq: len(dstEvents), Occurred: now, ID: record.DestinationAccountID, Amount: record.Amount, TransferID: record.ID, Purpose: "transfer", CounterpartyAccountID: record.SourceAccountID}
	srcEvents = append(srcEvents, debit)
	dstEvents = append(dstEvents, credit)
	m.streams[record.SourceAccountID] = srcEvents
	m.streams[record.DestinationAccountID] = dstEvents
	record.Status = domain.TransferCompleted
	record.FinalSourceBalance = record.InitialSourceBalance - record.Amount - record.Fee
	record.UpdatedAt = now
	m.transfers[record.ID] = record
	return &record, true, nil
}
func (m *MemoryStore) FindTransfer(id string) (*domain.TransferRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.transfers[id]
	if !ok {
		return nil, domain.ErrTransferNotFound
	}
	return &r, nil
}

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

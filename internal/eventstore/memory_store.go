package eventstore

import (
	"encoding/json"
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
	mu                  sync.RWMutex
	streams             map[string][]domain.Event
	snapshots           map[string]domain.AccountSnapshot
	customers           map[string]domain.Customer
	admins              map[string]domain.Admin
	sessions            map[string]domain.Session
	transfers           map[string]domain.TransferRecord
	adjustments         map[string]domain.AdjustmentRequest
	reservations        map[string]memoryReservation
	ledgerPostings      map[string]bool
	complianceDecisions map[string]string
	integrationEvents   []domain.IntegrationEvent
	nextIntegrationID   int64
}

type memoryReservation struct {
	AccountID string
	Amount    domain.Money
	State     string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		streams:             make(map[string][]domain.Event),
		snapshots:           make(map[string]domain.AccountSnapshot),
		customers:           make(map[string]domain.Customer),
		admins:              make(map[string]domain.Admin),
		sessions:            make(map[string]domain.Session),
		transfers:           make(map[string]domain.TransferRecord),
		adjustments:         make(map[string]domain.AdjustmentRequest),
		reservations:        make(map[string]memoryReservation),
		ledgerPostings:      make(map[string]bool),
		complianceDecisions: make(map[string]string),
		nextIntegrationID:   1,
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
var _ domain.TransferSagaStore = (*MemoryStore)(nil)
var _ domain.AdjustmentStore = (*MemoryStore)(nil)
var _ domain.EventBusStore = (*MemoryStore)(nil)

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
		if domain.ReplayAccount(account, events).AvailableBalance < amount {
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

func (m *MemoryStore) CreateTransfer(record domain.TransferRecord) (*domain.TransferRecord, bool, error) {
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
	m.transfers[record.ID] = record
	copy := record
	return &copy, true, nil
}
func (m *MemoryStore) ListDueTransfers(now time.Time, limit int) ([]domain.TransferRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []domain.TransferRecord{}
	for _, r := range m.transfers {
		if r.Status == domain.TransferPending && !r.NextAttemptAt.After(now) {
			out = append(out, r)
			if len(out) >= limit {
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (m *MemoryStore) UpdateTransferSaga(id string, u domain.TransferSagaUpdate, at time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.transfers[id]
	if !ok {
		return false, domain.ErrTransferNotFound
	}
	if r.Status != domain.TransferPending || r.CurrentStep != u.ExpectedStep {
		return false, nil
	}
	r.CurrentStep = u.CurrentStep
	r.Status = u.Status
	r.FailureCode = u.FailureCode
	if u.ComplianceStatus != "" {
		r.ComplianceStatus = u.ComplianceStatus
	}
	r.LastError = u.LastError
	r.AttemptCount = u.AttemptCount
	r.NextAttemptAt = u.NextAttemptAt
	r.UpdatedAt = at
	if u.InitialSourceBalance != 0 {
		r.InitialSourceBalance = u.InitialSourceBalance
	}
	if u.FinalSourceBalance != 0 {
		r.FinalSourceBalance = u.FinalSourceBalance
	}
	m.transfers[id] = r
	return true, nil
}
func (m *MemoryStore) ReserveTransferFunds(r domain.TransferRecord, at time.Time) (domain.Money, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.reservations[r.ID]; ok {
		return domain.ReplayAccount(r.SourceAccountID, m.streams[r.SourceAccountID]).Balance, nil
	}
	src, dst := m.streams[r.SourceAccountID], m.streams[r.DestinationAccountID]
	if len(src) == 0 || len(dst) == 0 {
		return 0, domain.ErrAccountNotFound
	}
	a := domain.ReplayAccount(r.SourceAccountID, src)
	amount := r.Amount + r.Fee
	if a.AvailableBalance < amount {
		return 0, domain.NewInsufficientFundsError(a, amount)
	}
	m.reservations[r.ID] = memoryReservation{AccountID: r.SourceAccountID, Amount: amount, State: "reserved"}
	m.streams[r.SourceAccountID] = append(src, domain.FundsReserved{Aggregate: r.SourceAccountID, Type: "FundsReserved", Seq: len(src), Occurred: at, ID: r.SourceAccountID, Amount: amount, TransferID: r.ID})
	return a.Balance, nil
}
func (m *MemoryStore) RecordComplianceDecision(id, decision string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.complianceDecisions[id]; ok && old != decision {
		return domain.ErrIdempotencyConflict
	}
	m.complianceDecisions[id] = decision
	return nil
}
func (m *MemoryStore) PostTransferLedger(r domain.TransferRecord, at time.Time) (domain.Money, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ledgerPostings[r.ID] {
		return domain.ReplayAccount(r.SourceAccountID, m.streams[r.SourceAccountID]).Balance, nil
	}
	src, dst := m.streams[r.SourceAccountID], m.streams[r.DestinationAccountID]
	if len(src) == 0 || len(dst) == 0 {
		return 0, domain.ErrAccountNotFound
	}
	if r.Fee > 0 {
		src = append(src, domain.MoneyDebited{Aggregate: r.SourceAccountID, Type: "MoneyDebited", Seq: len(src), Occurred: at, ID: r.SourceAccountID, Amount: r.Fee, TransferID: r.ID, Purpose: "fee", CounterpartyAccountID: r.DestinationAccountID})
	}
	src = append(src, domain.MoneyDebited{Aggregate: r.SourceAccountID, Type: "MoneyDebited", Seq: len(src), Occurred: at, ID: r.SourceAccountID, Amount: r.Amount, TransferID: r.ID, Purpose: "transfer", CounterpartyAccountID: r.DestinationAccountID})
	dst = append(dst, domain.MoneyCredited{Aggregate: r.DestinationAccountID, Type: "MoneyCredited", Seq: len(dst), Occurred: at, ID: r.DestinationAccountID, Amount: r.Amount, TransferID: r.ID, Purpose: "transfer", CounterpartyAccountID: r.SourceAccountID})
	m.streams[r.SourceAccountID] = src
	m.streams[r.DestinationAccountID] = dst
	m.ledgerPostings[r.ID] = true
	payload, _ := json.Marshal(domain.TransferCompletedEvent{TransferID: r.ID, Timestamp: at, Amount: r.Amount, SourceAccountID: r.SourceAccountID, DestinationAccountID: r.DestinationAccountID, Fee: r.Fee})
	m.appendIntegrationEventUnsafe(domain.IntegrationEvent{TransferID: r.ID, EventType: "TransferCompleted", Payload: payload, Status: domain.IntegrationEventScheduled, NextAttemptAt: at, CreatedAt: at})
	return domain.ReplayAccount(r.SourceAccountID, src).Balance, nil
}
func (m *MemoryStore) CaptureTransferReservation(r domain.TransferRecord, at time.Time) error {
	return m.finishReservation(r, at, "captured")
}
func (m *MemoryStore) ReleaseTransferReservation(r domain.TransferRecord, at time.Time) error {
	return m.finishReservation(r, at, "released")
}
func (m *MemoryStore) finishReservation(r domain.TransferRecord, at time.Time, state string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	hold, ok := m.reservations[r.ID]
	if !ok {
		return nil
	}
	if hold.State == state {
		return nil
	}
	if hold.State != "reserved" {
		return domain.ErrIdempotencyConflict
	}
	events := m.streams[hold.AccountID]
	if state == "captured" {
		events = append(events, domain.ReservationCaptured{Aggregate: hold.AccountID, Type: "ReservationCaptured", Seq: len(events), Occurred: at, ID: hold.AccountID, Amount: hold.Amount, TransferID: r.ID})
	} else {
		events = append(events, domain.ReservationReleased{Aggregate: hold.AccountID, Type: "ReservationReleased", Seq: len(events), Occurred: at, ID: hold.AccountID, Amount: hold.Amount, TransferID: r.ID})
	}
	hold.State = state
	m.reservations[r.ID] = hold
	m.streams[hold.AccountID] = events
	return nil
}

func (m *MemoryStore) WithdrawAvailable(accountID string, amount domain.Money, at time.Time) (*domain.Account, *domain.Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	events := m.streams[accountID]
	if len(events) == 0 {
		return nil, nil, domain.ErrAccountNotFound
	}
	initial := domain.ReplayAccount(accountID, events)
	if initial.AvailableBalance < amount {
		return nil, nil, domain.NewInsufficientFundsError(initial, amount)
	}
	ev := domain.MoneyDebited{Aggregate: accountID, Type: "MoneyDebited", Seq: len(events), Occurred: at, ID: accountID, Amount: amount}
	m.streams[accountID] = append(events, ev)
	return initial, domain.ReplayAccount(accountID, append(events, ev)), nil
}

// ExecuteTransfer is retained for compatibility with older callers. New transfers use the saga methods above.
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

// appendIntegrationEventUnsafe enqueues a durable outbox row. Callers must hold
// m.mu (write). It is idempotent on (transfer_id, event_type), so a retried
// ledger post never enqueues the event twice.
func (m *MemoryStore) appendIntegrationEventUnsafe(event domain.IntegrationEvent) {
	for _, existing := range m.integrationEvents {
		if existing.TransferID == event.TransferID && existing.EventType == event.EventType {
			return
		}
	}
	event.ID = m.nextIntegrationID
	m.nextIntegrationID++
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if event.Status == "" {
		event.Status = domain.IntegrationEventScheduled
	}
	m.integrationEvents = append(m.integrationEvents, event)
}

// AppendIntegrationEvent enqueues a durable outbox row.
func (m *MemoryStore) AppendIntegrationEvent(event domain.IntegrationEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendIntegrationEventUnsafe(event)
	return nil
}

func (m *MemoryStore) ListDueIntegrationEvents(now time.Time, limit int) ([]domain.IntegrationEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []domain.IntegrationEvent{}
	for _, e := range m.integrationEvents {
		if e.Status == domain.IntegrationEventScheduled && !e.NextAttemptAt.After(now) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryStore) MarkIntegrationEventPublished(id int64, at time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.integrationEvents {
		if m.integrationEvents[i].ID == id {
			if m.integrationEvents[i].Status != domain.IntegrationEventScheduled {
				return false, nil
			}
			m.integrationEvents[i].Status = domain.IntegrationEventPublished
			m.integrationEvents[i].PublishedAt = &at
			return true, nil
		}
	}
	return false, nil
}

func (m *MemoryStore) RecordIntegrationEventFailure(event domain.IntegrationEvent, nextAttemptAt time.Time, deadLetter bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.integrationEvents {
		if m.integrationEvents[i].ID == event.ID {
			if m.integrationEvents[i].Status != domain.IntegrationEventScheduled {
				return nil
			}
			m.integrationEvents[i].AttemptCount = event.AttemptCount
			m.integrationEvents[i].LastError = event.LastError
			if deadLetter {
				m.integrationEvents[i].Status = domain.IntegrationEventDeadLetter
			} else {
				m.integrationEvents[i].NextAttemptAt = nextAttemptAt
			}
			return nil
		}
	}
	return nil
}

func (m *MemoryStore) RequeueIntegrationEvent(id int64, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.integrationEvents {
		if m.integrationEvents[i].ID == id {
			if m.integrationEvents[i].Status != domain.IntegrationEventDeadLetter {
				return nil
			}
			m.integrationEvents[i].Status = domain.IntegrationEventScheduled
			m.integrationEvents[i].AttemptCount = 0
			m.integrationEvents[i].LastError = ""
			m.integrationEvents[i].NextAttemptAt = at
			m.integrationEvents[i].PublishedAt = nil
			return nil
		}
	}
	return nil
}

func (m *MemoryStore) ListIntegrationEvents(status string, limit int) ([]domain.IntegrationEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []domain.IntegrationEvent{}
	for _, e := range m.integrationEvents {
		if status == "" || e.Status == status {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
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

// SaveSnapshot stores the latest account snapshot for an aggregate, ignoring any
// snapshot whose AsOfSequence is not newer than the one already stored.
func (m *MemoryStore) SaveSnapshot(snap domain.AccountSnapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.snapshots[snap.AggregateID]; ok && existing.AsOfSequence >= snap.AsOfSequence {
		return nil
	}
	m.snapshots[snap.AggregateID] = snap
	return nil
}

// LoadSnapshot returns a copy of the latest account snapshot, or nil and
// ErrNoSnapshot when none exists.
func (m *MemoryStore) LoadSnapshot(aggregateID string) (*domain.AccountSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snap, ok := m.snapshots[aggregateID]
	if !ok {
		return nil, ErrNoSnapshot
	}
	return &snap, nil
}

var _ Store = (*MemoryStore)(nil)

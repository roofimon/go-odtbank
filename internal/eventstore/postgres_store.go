package eventstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-odtbank/internal/domain"
)

// PostgresStore is a Postgres-backed implementation of Store.
// Rows are stored in the `events` table created by migrations/0001_init.up.sql.
//
// Optimistic concurrency is enforced by (aggregate_id, sequence) being the
// primary key: a duplicate insert is silently skipped by ON CONFLICT DO NOTHING,
// and we surface that as ErrConcurrencyConflict.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Close releases the underlying connection pool.
func (s *PostgresStore) Close() {
	s.pool.Close()
}

func (s *PostgresStore) CreateCustomerApplication(customer domain.Customer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("eventstore: begin onboarding: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO customers (
			id, account_id, legal_first_name, legal_last_name, date_of_birth,
			nationality, email, phone, password_hash, address_line1, address_line2, city,
			state_or_province, postal_code, country, document_type,
			document_number, document_issuing_country, passport_image,
			passport_image_mime, kyc_status, requested_initial_deposit, created_at
		) VALUES ($1,NULL,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
	`, customer.ID, customer.LegalFirstName, customer.LegalLastName,
		customer.DateOfBirth, customer.Nationality, customer.Email, customer.Phone, customer.PasswordHash,
		customer.ResidentialAddress.Line1, customer.ResidentialAddress.Line2,
		customer.ResidentialAddress.City, customer.ResidentialAddress.StateOrProvince,
		customer.ResidentialAddress.PostalCode, customer.ResidentialAddress.Country,
		customer.GovernmentDocument.Type, customer.GovernmentDocument.Number,
		customer.GovernmentDocument.IssuingCountry, customer.PassportImage,
		customer.PassportImageMIME, customer.KYCStatus, customer.RequestedDeposit, customer.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrCustomerAlreadyExists
		}
		return fmt.Errorf("eventstore: insert customer: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("eventstore: commit onboarding: %w", err)
	}
	return nil
}

func (s *PostgresStore) FindCustomerByEmail(email string) (*domain.Customer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var customer domain.Customer
	err := s.pool.QueryRow(ctx, `SELECT id, COALESCE(account_id,''), email, password_hash, kyc_status, rejection_reason FROM customers WHERE lower(email)=lower($1)`, email).
		Scan(&customer.ID, &customer.AccountID, &customer.Email, &customer.PasswordHash, &customer.KYCStatus, &customer.RejectionReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("eventstore: find customer: %w", err)
	}
	return &customer, nil
}

func (s *PostgresStore) FindAdminByEmail(email string) (*domain.Admin, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var a domain.Admin
	err := s.pool.QueryRow(ctx, `SELECT id,email,password_hash FROM admins WHERE lower(email)=lower($1)`, email).Scan(&a.ID, &a.Email, &a.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("eventstore: find admin: %w", err)
	}
	return &a, nil
}
func (s *PostgresStore) UpsertAdmin(a domain.Admin) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.pool.Exec(ctx, `INSERT INTO admins(id,email,password_hash) VALUES($1,$2,$3) ON CONFLICT (lower(email)) DO UPDATE SET password_hash=EXCLUDED.password_hash`, a.ID, a.Email, a.PasswordHash)
	return err
}

func (s *PostgresStore) CreateSession(session domain.Session) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.pool.Exec(ctx, `INSERT INTO sessions (token_hash, customer_id, account_id, admin_id, expires_at) VALUES ($1,NULLIF($2,''),NULL,NULLIF($3,''),$4)`, session.TokenHash, session.CustomerID, session.AdminID, session.ExpiresAt)
	if err != nil {
		return fmt.Errorf("eventstore: create session: %w", err)
	}
	return nil
}

func (s *PostgresStore) FindSession(tokenHash string, now time.Time) (*domain.Principal, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var principal domain.Principal
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(s.customer_id,''),COALESCE(c.account_id,''),COALESCE(s.admin_id,''),COALESCE(c.email,a.email),CASE WHEN s.admin_id IS NULL THEN 'customer' ELSE 'admin' END,COALESCE(c.kyc_status,''),COALESCE(c.rejection_reason,'') FROM sessions s LEFT JOIN customers c ON c.id=s.customer_id LEFT JOIN admins a ON a.id=s.admin_id WHERE s.token_hash=$1 AND s.expires_at>$2`, tokenHash, now).
		Scan(&principal.CustomerID, &principal.AccountID, &principal.AdminID, &principal.Email, &principal.Role, &principal.KYCStatus, &principal.RejectionReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrUnauthorized
	}
	if err != nil {
		return nil, fmt.Errorf("eventstore: find session: %w", err)
	}
	return &principal, nil
}

func (s *PostgresStore) DeleteSession(tokenHash string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, tokenHash)
	if err != nil {
		return fmt.Errorf("eventstore: delete session: %w", err)
	}
	return nil
}

func (s *PostgresStore) ListApplications(status string) ([]domain.ApplicationSummary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.pool.Query(ctx, `SELECT id,legal_first_name,legal_last_name,email,kyc_status,requested_initial_deposit,created_at,reviewed_at,rejection_reason FROM customers WHERE kyc_status=$1 ORDER BY created_at`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ApplicationSummary{}
	for rows.Next() {
		var a domain.ApplicationSummary
		if err := rows.Scan(&a.CustomerID, &a.LegalFirstName, &a.LegalLastName, &a.Email, &a.KYCStatus, &a.RequestedDeposit, &a.CreatedAt, &a.ReviewedAt, &a.RejectionReason); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *PostgresStore) GetApplication(id string) (*domain.ApplicationDetail, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var a domain.ApplicationDetail
	err := s.pool.QueryRow(ctx, `SELECT id,legal_first_name,legal_last_name,email,kyc_status,requested_initial_deposit,created_at,reviewed_at,rejection_reason,date_of_birth,nationality,phone,address_line1,address_line2,city,state_or_province,postal_code,country,document_type,document_number,document_issuing_country,passport_image_mime FROM customers WHERE id=$1`, id).Scan(&a.CustomerID, &a.LegalFirstName, &a.LegalLastName, &a.Email, &a.KYCStatus, &a.RequestedDeposit, &a.CreatedAt, &a.ReviewedAt, &a.RejectionReason, &a.DateOfBirth, &a.Nationality, &a.Phone, &a.ResidentialAddress.Line1, &a.ResidentialAddress.Line2, &a.ResidentialAddress.City, &a.ResidentialAddress.StateOrProvince, &a.ResidentialAddress.PostalCode, &a.ResidentialAddress.Country, &a.GovernmentDocument.Type, &a.GovernmentDocument.Number, &a.GovernmentDocument.IssuingCountry, &a.PassportImageMIME)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrApplicationNotFound
	}
	return &a, err
}
func (s *PostgresStore) GetPassportImage(id string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var b []byte
	var mime string
	err := s.pool.QueryRow(ctx, `SELECT passport_image,passport_image_mime FROM customers WHERE id=$1`, id).Scan(&b, &mime)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", domain.ErrApplicationNotFound
	}
	return b, mime, err
}
func (s *PostgresStore) ApproveApplication(id, adminID, accountID string, at time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	var amount domain.Money
	err = tx.QueryRow(ctx, `SELECT kyc_status,requested_initial_deposit FROM customers WHERE id=$1 FOR UPDATE`, id).Scan(&status, &amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrApplicationNotFound
	}
	if err != nil {
		return err
	}
	if status != domain.KYCWaiting {
		return domain.ErrApplicationReviewed
	}
	opened := domain.AccountOpened{Aggregate: accountID, Type: "AccountOpened", Seq: 0, Occurred: at, ID: accountID, InitialBalance: amount}
	payload, err := json.Marshal(opened)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO events(aggregate_id,sequence,event_type,payload,occurred_at) VALUES($1,0,$2,$3,$4)`, accountID, opened.EventType(), payload, at); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE customers SET account_id=$2,kyc_status=$3,reviewed_by=$4,reviewed_at=$5 WHERE id=$1`, id, accountID, domain.KYCApproved, adminID, at)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *PostgresStore) RejectApplication(id, adminID, reason string, at time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, err := s.pool.Exec(ctx, `UPDATE customers SET kyc_status=$2,reviewed_by=$3,reviewed_at=$4,rejection_reason=$5 WHERE id=$1 AND kyc_status=$6`, id, domain.KYCRejected, adminID, at, reason, domain.KYCWaiting)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM customers WHERE id=$1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return domain.ErrApplicationNotFound
		}
		return domain.ErrApplicationReviewed
	}
	return nil
}

var _ domain.ReviewStore = (*PostgresStore)(nil)
var _ domain.AtomicTransferStore = (*PostgresStore)(nil)

func (s *PostgresStore) ExecuteTransfer(record domain.TransferRecord) (*domain.TransferRecord, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.pool.Exec(ctx, `INSERT INTO transfers(id,source_account_id,destination_account_id,idempotency_key,amount_minor,fee_minor,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,'pending',$7,$7)`, record.ID, record.SourceAccountID, record.DestinationAccountID, record.IdempotencyKey, int64(record.Amount), int64(record.Fee), record.CreatedAt)
	created := err == nil
	if err != nil {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			return nil, false, err
		}
		existing, e := s.findTransferByKey(ctx, record.SourceAccountID, record.IdempotencyKey)
		if e != nil {
			return nil, false, e
		}
		if existing.Amount != record.Amount || existing.DestinationAccountID != record.DestinationAccountID {
			return nil, false, domain.ErrIdempotencyConflict
		}
		if existing.Status != domain.TransferPending {
			return existing, false, nil
		}
		record = *existing
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, minString(record.SourceAccountID, record.DestinationAccountID)); err != nil {
		return nil, false, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, maxString(record.SourceAccountID, record.DestinationAccountID)); err != nil {
		return nil, false, err
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM transfers WHERE id=$1 FOR UPDATE`, record.ID).Scan(&status); err != nil {
		return nil, false, err
	}
	if status != domain.TransferPending {
		tx.Rollback(ctx)
		existing, findErr := s.FindTransfer(record.ID)
		return existing, false, findErr
	}
	srcEvents, err := loadEventsTx(ctx, tx, record.SourceAccountID)
	if err != nil {
		return nil, false, err
	}
	dstEvents, err := loadEventsTx(ctx, tx, record.DestinationAccountID)
	if err != nil {
		return nil, false, err
	}
	if len(srcEvents) == 0 || len(dstEvents) == 0 {
		return s.failTransferTx(ctx, tx, record, "account_not_found", created)
	}
	src := domain.ReplayAccount(record.SourceAccountID, srcEvents)
	record.InitialSourceBalance = src.Balance
	if src.Balance < record.Amount+record.Fee {
		return s.failTransferTx(ctx, tx, record, "insufficient_funds", created)
	}
	now := time.Now().UTC()
	if record.Fee > 0 {
		e := domain.MoneyDebited{Aggregate: record.SourceAccountID, Type: "MoneyDebited", Seq: len(srcEvents), Occurred: now, ID: record.SourceAccountID, Amount: record.Fee, TransferID: record.ID, Purpose: "fee", CounterpartyAccountID: record.DestinationAccountID}
		if err = insertEventTx(ctx, tx, e); err != nil {
			return nil, false, err
		}
		srcEvents = append(srcEvents, e)
	}
	debit := domain.MoneyDebited{Aggregate: record.SourceAccountID, Type: "MoneyDebited", Seq: len(srcEvents), Occurred: now, ID: record.SourceAccountID, Amount: record.Amount, TransferID: record.ID, Purpose: "transfer", CounterpartyAccountID: record.DestinationAccountID}
	credit := domain.MoneyCredited{Aggregate: record.DestinationAccountID, Type: "MoneyCredited", Seq: len(dstEvents), Occurred: now, ID: record.DestinationAccountID, Amount: record.Amount, TransferID: record.ID, Purpose: "transfer", CounterpartyAccountID: record.SourceAccountID}
	if err = insertEventTx(ctx, tx, debit); err != nil {
		return nil, false, err
	}
	if err = insertEventTx(ctx, tx, credit); err != nil {
		return nil, false, err
	}
	record.Status = domain.TransferCompleted
	record.FinalSourceBalance = record.InitialSourceBalance - record.Amount - record.Fee
	record.UpdatedAt = now
	_, err = tx.Exec(ctx, `UPDATE transfers SET status=$2,initial_source_balance_minor=$3,final_source_balance_minor=$4,updated_at=$5 WHERE id=$1`, record.ID, record.Status, int64(record.InitialSourceBalance), int64(record.FinalSourceBalance), now)
	if err != nil {
		return nil, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return &record, true, nil
}
func minString(a, b string) string {
	if a < b {
		return a
	}
	return b
}
func maxString(a, b string) string {
	if a > b {
		return a
	}
	return b
}
func loadEventsTx(ctx context.Context, tx pgx.Tx, id string) ([]domain.Event, error) {
	rows, err := tx.Query(ctx, `SELECT event_type,payload FROM events WHERE aggregate_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Event{}
	for rows.Next() {
		var typ string
		var payload []byte
		if err = rows.Scan(&typ, &payload); err != nil {
			return nil, err
		}
		e, decodeErr := decodeEvent(id, typ, payload)
		if decodeErr != nil {
			return nil, decodeErr
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func insertEventTx(ctx context.Context, tx pgx.Tx, e domain.Event) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO events(aggregate_id,sequence,event_type,payload,occurred_at) VALUES($1,$2,$3,$4,$5)`, e.AggregateID(), e.Version(), e.EventType(), payload, e.OccurredAt())
	return err
}
func (s *PostgresStore) failTransferTx(ctx context.Context, tx pgx.Tx, r domain.TransferRecord, code string, created bool) (*domain.TransferRecord, bool, error) {
	r.Status = domain.TransferFailed
	r.FailureCode = code
	r.UpdatedAt = time.Now().UTC()
	_, err := tx.Exec(ctx, `UPDATE transfers SET status='failed',failure_code=$2,initial_source_balance_minor=$3,updated_at=$4 WHERE id=$1`, r.ID, code, int64(r.InitialSourceBalance), r.UpdatedAt)
	if err != nil {
		return nil, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return &r, created, nil
}
func (s *PostgresStore) findTransferByKey(ctx context.Context, source, key string) (*domain.TransferRecord, error) {
	var r domain.TransferRecord
	err := s.pool.QueryRow(ctx, `SELECT id,source_account_id,destination_account_id,idempotency_key,amount_minor,fee_minor,status,failure_code,initial_source_balance_minor,final_source_balance_minor,created_at,updated_at FROM transfers WHERE source_account_id=$1 AND idempotency_key=$2`, source, key).Scan(&r.ID, &r.SourceAccountID, &r.DestinationAccountID, &r.IdempotencyKey, &r.Amount, &r.Fee, &r.Status, &r.FailureCode, &r.InitialSourceBalance, &r.FinalSourceBalance, &r.CreatedAt, &r.UpdatedAt)
	return &r, err
}
func (s *PostgresStore) FindTransfer(id string) (*domain.TransferRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var r domain.TransferRecord
	err := s.pool.QueryRow(ctx, `SELECT id,source_account_id,destination_account_id,idempotency_key,amount_minor,fee_minor,status,failure_code,initial_source_balance_minor,final_source_balance_minor,created_at,updated_at FROM transfers WHERE id=$1`, id).Scan(&r.ID, &r.SourceAccountID, &r.DestinationAccountID, &r.IdempotencyKey, &r.Amount, &r.Fee, &r.Status, &r.FailureCode, &r.InitialSourceBalance, &r.FinalSourceBalance, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrTransferNotFound
	}
	return &r, err
}

// Append inserts a single event. expectedVersion must equal the current
// stream length for the aggregate; a mismatch returns ErrConcurrencyConflict.
func (s *PostgresStore) Append(event domain.Event, expectedVersion int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("eventstore: marshal event: %w", err)
	}

	tag, err := s.pool.Exec(ctx, `
		INSERT INTO events (aggregate_id, sequence, event_type, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (aggregate_id, sequence) DO NOTHING
	`,
		event.AggregateID(),
		expectedVersion,
		event.EventType(),
		payload,
		event.OccurredAt(),
	)
	if err != nil {
		return fmt.Errorf("eventstore: insert event: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConcurrencyConflict
	}
	return nil
}

// Load returns every event for the aggregate, ordered by sequence.
func (s *PostgresStore) Load(aggregateID string) ([]domain.Event, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := s.pool.Query(ctx, `
		SELECT event_type, payload
		FROM events
		WHERE aggregate_id = $1
		ORDER BY sequence
	`, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("eventstore: query events: %w", err)
	}
	defer rows.Close()

	var out []domain.Event
	for rows.Next() {
		var eventType string
		var payload []byte
		if err := rows.Scan(&eventType, &payload); err != nil {
			return nil, fmt.Errorf("eventstore: scan event: %w", err)
		}
		ev, err := decodeEvent(aggregateID, eventType, payload)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("eventstore: iterate events: %w", err)
	}
	return out, nil
}

// ListAggregates returns distinct aggregate IDs, sorted ascending.
func (s *PostgresStore) ListAggregates() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT aggregate_id
		FROM events
		ORDER BY aggregate_id
	`)
	if err != nil {
		return nil, fmt.Errorf("eventstore: list aggregates: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("eventstore: scan aggregate: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("eventstore: iterate aggregates: %w", err)
	}
	return out, nil
}

// decodeEvent materializes the stored payload into the correct concrete type
// based on event_type. JSON field names match the struct field names.
func decodeEvent(aggregateID, eventType string, payload []byte) (domain.Event, error) {
	switch eventType {
	case "AccountOpened":
		var e domain.AccountOpened
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, fmt.Errorf("eventstore: decode AccountOpened: %w", err)
		}
		e.Aggregate = aggregateID
		return e, nil
	case "MoneyDebited":
		var e domain.MoneyDebited
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, fmt.Errorf("eventstore: decode MoneyDebited: %w", err)
		}
		e.Aggregate = aggregateID
		return e, nil
	case "MoneyCredited":
		var e domain.MoneyCredited
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, fmt.Errorf("eventstore: decode MoneyCredited: %w", err)
		}
		e.Aggregate = aggregateID
		return e, nil
	default:
		return nil, fmt.Errorf("eventstore: unknown event type %q", eventType)
	}
}

// Compile-time check that PostgresStore satisfies Store.
var _ Store = (*PostgresStore)(nil)
var _ domain.OnboardingStore = (*PostgresStore)(nil)
var _ domain.AuthStore = (*PostgresStore)(nil)

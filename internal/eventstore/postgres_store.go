package eventstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
	pool            *pgxpool.Pool
	passportObjects PassportObjectStore
}

type PassportObjectStore interface {
	Put(ctx context.Context, key string, body []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

func NewPostgresStore(pool *pgxpool.Pool, passportObjects ...PassportObjectStore) *PostgresStore {
	store := &PostgresStore{pool: pool}
	if len(passportObjects) > 0 {
		store.passportObjects = passportObjects[0]
	}
	return store
}

// Close releases the underlying connection pool.
func (s *PostgresStore) Close() {
	s.pool.Close()
}

func (s *PostgresStore) CreateCustomerApplication(customer domain.Customer) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	objectKey := ""
	passportImage := customer.PassportImage
	if s.passportObjects != nil {
		objectKey = "passports/" + customer.ID
		if err := s.passportObjects.Put(ctx, objectKey, customer.PassportImage, customer.PassportImageMIME); err != nil {
			return err
		}
		passportImage = []byte{}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		if objectKey != "" {
			_ = s.passportObjects.Delete(context.Background(), objectKey)
		}
		return fmt.Errorf("eventstore: begin onboarding: %w", err)
	}
	defer tx.Rollback(ctx)
	committed := false
	defer func() {
		if objectKey != "" && !committed {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cleanupCancel()
			_ = s.passportObjects.Delete(cleanupCtx, objectKey)
		}
	}()

	_, err = tx.Exec(ctx, `
		INSERT INTO customers (
			id, account_id, legal_first_name, legal_last_name, date_of_birth,
			nationality, email, phone, password_hash, address_line1, address_line2, city,
			state_or_province, postal_code, country, document_type,
			document_number, document_issuing_country, passport_image,
			passport_image_mime, passport_object_key, kyc_status, requested_initial_deposit, created_at
		) VALUES ($1,NULL,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
	`, customer.ID, customer.LegalFirstName, customer.LegalLastName,
		customer.DateOfBirth, customer.Nationality, customer.Email, customer.Phone, customer.PasswordHash,
		customer.ResidentialAddress.Line1, customer.ResidentialAddress.Line2,
		customer.ResidentialAddress.City, customer.ResidentialAddress.StateOrProvince,
		customer.ResidentialAddress.PostalCode, customer.ResidentialAddress.Country,
		customer.GovernmentDocument.Type, customer.GovernmentDocument.Number,
		customer.GovernmentDocument.IssuingCountry, passportImage,
		customer.PassportImageMIME, objectKey, customer.KYCStatus, customer.RequestedDeposit, customer.CreatedAt)
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
	committed = true
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
	var mime, objectKey string
	err := s.pool.QueryRow(ctx, `SELECT passport_image,passport_image_mime,passport_object_key FROM customers WHERE id=$1`, id).Scan(&b, &mime, &objectKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", domain.ErrApplicationNotFound
	}
	if err != nil {
		return nil, "", err
	}
	if objectKey != "" {
		if s.passportObjects == nil {
			return nil, "", errors.New("passport object storage is not configured")
		}
		b, err = s.passportObjects.Get(ctx, objectKey)
		if err != nil {
			return nil, "", err
		}
	}
	return b, mime, nil
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
var _ domain.TransferSagaStore = (*PostgresStore)(nil)
var _ domain.AdjustmentStore = (*PostgresStore)(nil)
var _ domain.EventBusStore = (*PostgresStore)(nil)

func (s *PostgresStore) CreateAdjustment(r domain.AdjustmentRequest) (*domain.AdjustmentRequest, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if r.Type == domain.AdjustmentManual {
		events, err := s.Load(r.AccountID)
		if err != nil {
			return nil, err
		}
		if len(events) == 0 {
			return nil, domain.ErrAccountNotFound
		}
	} else if r.OriginalTransferID != "" {
		t, err := s.FindTransfer(r.OriginalTransferID)
		if err != nil || t.Status != domain.TransferCompleted {
			return nil, domain.ErrTransferNotFound
		}
		var exists bool
		if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM adjustment_requests WHERE original_transfer_id=$1 AND status<>'rejected')`, r.OriginalTransferID).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			return nil, domain.ErrAlreadyReversed
		}
		r.AccountID = t.SourceAccountID
		r.CounterpartyAccountID = t.DestinationAccountID
		r.Amount = t.Amount
		r.Fee = t.Fee
		r.Direction = "reversal"
	} else {
		events, err := s.Load(r.OriginalAccountID)
		if err != nil {
			return nil, err
		}
		if r.OriginalEventSequence == nil || *r.OriginalEventSequence < 0 || *r.OriginalEventSequence >= len(events) {
			return nil, domain.ErrInvalidAdjustment
		}
		var exists bool
		if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM adjustment_requests WHERE original_account_id=$1 AND original_event_sequence=$2 AND status<>'rejected')`, r.OriginalAccountID, *r.OriginalEventSequence).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			return nil, domain.ErrAlreadyReversed
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
	var seq any
	if r.OriginalEventSequence != nil {
		seq = *r.OriginalEventSequence
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO adjustment_requests(id,adjustment_type,status,account_id,direction,amount_minor,fee_minor,counterparty_account_id,original_transfer_id,original_account_id,original_event_sequence,reason,case_reference,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),$11,$12,$13,$14,$15)`, r.ID, r.Type, r.Status, r.AccountID, r.Direction, int64(r.Amount), int64(r.Fee), r.CounterpartyAccountID, r.OriginalTransferID, r.OriginalAccountID, seq, r.Reason, r.CaseReference, r.CreatedBy, r.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && (pgErr.ConstraintName == "adjustment_requests_active_transfer_reversal_idx" || pgErr.ConstraintName == "adjustment_requests_active_event_reversal_idx") {
			return nil, domain.ErrAlreadyReversed
		}
		return nil, err
	}
	return &r, nil
}
func scanAdjustment(row pgx.Row) (*domain.AdjustmentRequest, error) {
	var r domain.AdjustmentRequest
	var seq *int64
	err := row.Scan(&r.ID, &r.Type, &r.Status, &r.AccountID, &r.Direction, &r.Amount, &r.Fee, &r.CounterpartyAccountID, &r.OriginalTransferID, &r.OriginalAccountID, &seq, &r.Reason, &r.CaseReference, &r.CreatedBy, &r.ReviewedBy, &r.RejectionReason, &r.CreatedAt, &r.ReviewedAt)
	if seq != nil {
		v := int(*seq)
		r.OriginalEventSequence = &v
	}
	return &r, err
}

const adjustmentColumns = `id,adjustment_type,status,account_id,direction,amount_minor,fee_minor,counterparty_account_id,COALESCE(original_transfer_id,''),COALESCE(original_account_id,''),original_event_sequence,reason,case_reference,created_by,COALESCE(reviewed_by,''),rejection_reason,created_at,reviewed_at`

func (s *PostgresStore) GetAdjustment(id string) (*domain.AdjustmentRequest, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := scanAdjustment(s.pool.QueryRow(ctx, `SELECT `+adjustmentColumns+` FROM adjustment_requests WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrAdjustmentNotFound
	}
	return r, err
}
func (s *PostgresStore) ListAdjustments(status string) ([]domain.AdjustmentRequest, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.pool.Query(ctx, `SELECT `+adjustmentColumns+` FROM adjustment_requests WHERE status=$1 ORDER BY created_at`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.AdjustmentRequest{}
	for rows.Next() {
		r, scanErr := scanAdjustment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}
func (s *PostgresStore) ApproveAdjustment(id, adminID string, at time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	r, err := scanAdjustment(tx.QueryRow(ctx, `SELECT `+adjustmentColumns+` FROM adjustment_requests WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrAdjustmentNotFound
	}
	if err != nil {
		return err
	}
	if r.Status != domain.AdjustmentWaiting {
		return domain.ErrAdjustmentReviewed
	}
	if r.CreatedBy == adminID {
		return domain.ErrSelfApproval
	}
	accounts := []string{r.AccountID}
	if r.CounterpartyAccountID != "" {
		accounts = append(accounts, r.CounterpartyAccountID)
	}
	sort.Strings(accounts)
	for _, account := range accounts {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, account); err != nil {
			return err
		}
	}
	if r.Type == domain.AdjustmentReversal {
		var alreadyApproved bool
		if r.OriginalTransferID != "" {
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM adjustment_requests WHERE id<>$1 AND original_transfer_id=$2 AND status='approved')`, r.ID, r.OriginalTransferID).Scan(&alreadyApproved)
		} else {
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM adjustment_requests WHERE id<>$1 AND original_account_id=$2 AND original_event_sequence=$3 AND status='approved')`, r.ID, r.OriginalAccountID, *r.OriginalEventSequence).Scan(&alreadyApproved)
		}
		if err != nil {
			return err
		}
		if alreadyApproved {
			return domain.ErrAlreadyReversed
		}
	}
	ref := r.OriginalTransferID
	if ref == "" && r.OriginalEventSequence != nil {
		ref = fmt.Sprintf("%s:%d", r.OriginalAccountID, *r.OriginalEventSequence)
	}
	purpose := "adjustment"
	if r.Type == domain.AdjustmentReversal {
		purpose = "reversal"
	}
	credit := func(account string, amount domain.Money, counterparty string) error {
		events, e := loadEventsTx(ctx, tx, account)
		if e != nil {
			return e
		}
		return insertEventTx(ctx, tx, domain.MoneyCredited{Aggregate: account, Type: "MoneyCredited", Seq: len(events), Occurred: at, ID: account, Amount: amount, Purpose: purpose, CounterpartyAccountID: counterparty, AdjustmentID: r.ID, AdjustmentReason: r.Reason, CaseReference: r.CaseReference, OriginalReference: ref})
	}
	debit := func(account string, amount domain.Money, counterparty string) error {
		events, e := loadEventsTx(ctx, tx, account)
		if e != nil {
			return e
		}
		a := domain.ReplayAccount(account, events)
		if a.AvailableBalance < amount {
			return domain.NewInsufficientFundsError(a, amount)
		}
		return insertEventTx(ctx, tx, domain.MoneyDebited{Aggregate: account, Type: "MoneyDebited", Seq: len(events), Occurred: at, ID: account, Amount: amount, Purpose: purpose, CounterpartyAccountID: counterparty, AdjustmentID: r.ID, AdjustmentReason: r.Reason, CaseReference: r.CaseReference, OriginalReference: ref})
	}
	if r.OriginalTransferID != "" {
		if err = debit(r.CounterpartyAccountID, r.Amount, r.AccountID); err != nil {
			return err
		}
		if err = credit(r.AccountID, r.Amount+r.Fee, r.CounterpartyAccountID); err != nil {
			return err
		}
	} else if r.Direction == "credit" {
		err = credit(r.AccountID, r.Amount, "")
	} else {
		err = debit(r.AccountID, r.Amount, "")
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE adjustment_requests SET status=$2,reviewed_by=$3,reviewed_at=$4 WHERE id=$1`, id, domain.AdjustmentApproved, adminID, at)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *PostgresStore) RejectAdjustment(id, adminID, reason string, at time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status, maker string
	err = tx.QueryRow(ctx, `SELECT status,created_by FROM adjustment_requests WHERE id=$1 FOR UPDATE`, id).Scan(&status, &maker)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrAdjustmentNotFound
	}
	if err != nil {
		return err
	}
	if status != domain.AdjustmentWaiting {
		return domain.ErrAdjustmentReviewed
	}
	if maker == adminID {
		return domain.ErrSelfApproval
	}
	_, err = tx.Exec(ctx, `UPDATE adjustment_requests SET status=$2,reviewed_by=$3,rejection_reason=$4,reviewed_at=$5 WHERE id=$1`, id, domain.AdjustmentRejected, adminID, reason, at)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

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

func (s *PostgresStore) CreateTransfer(r domain.TransferRecord) (*domain.TransferRecord, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.pool.Exec(ctx, `INSERT INTO transfers(id,source_account_id,destination_account_id,idempotency_key,amount_minor,fee_minor,status,current_step,next_attempt_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)`, r.ID, r.SourceAccountID, r.DestinationAccountID, r.IdempotencyKey, int64(r.Amount), int64(r.Fee), r.Status, r.CurrentStep, r.NextAttemptAt, r.CreatedAt)
	if err == nil {
		return &r, true, nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return nil, false, err
	}
	existing, e := s.findTransferByKey(ctx, r.SourceAccountID, r.IdempotencyKey)
	if e != nil {
		return nil, false, e
	}
	if existing.Amount != r.Amount || existing.DestinationAccountID != r.DestinationAccountID {
		return nil, false, domain.ErrIdempotencyConflict
	}
	return existing, false, nil
}
func (s *PostgresStore) ListDueTransfers(now time.Time, limit int) ([]domain.TransferRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, e := s.pool.Query(ctx, `SELECT `+transferColumns+` FROM transfers WHERE status='pending' AND next_attempt_at<=$1 ORDER BY next_attempt_at,created_at LIMIT $2`, now, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.TransferRecord{}
	for rows.Next() {
		r, e := scanTransfer(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}
func (s *PostgresStore) UpdateTransferSaga(id string, u domain.TransferSagaUpdate, at time.Time) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, e := s.pool.Exec(ctx, `UPDATE transfers SET current_step=$3,status=$4,failure_code=$5,compliance_status=CASE WHEN $6='' THEN compliance_status ELSE $6 END,last_error=$7,attempt_count=$8,next_attempt_at=$9,initial_source_balance_minor=CASE WHEN $10=0 THEN initial_source_balance_minor ELSE $10 END,final_source_balance_minor=CASE WHEN $11=0 THEN final_source_balance_minor ELSE $11 END,updated_at=$12 WHERE id=$1 AND current_step=$2 AND status='pending'`, id, u.ExpectedStep, u.CurrentStep, u.Status, u.FailureCode, u.ComplianceStatus, u.LastError, u.AttemptCount, u.NextAttemptAt, int64(u.InitialSourceBalance), int64(u.FinalSourceBalance), at)
	return tag.RowsAffected() == 1, e
}

func lockAccountTx(ctx context.Context, tx pgx.Tx, id string) error {
	_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, id)
	return e
}
func (s *PostgresStore) ReserveTransferFunds(r domain.TransferRecord, at time.Time) (domain.Money, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(ctx)
	if e = lockAccountTx(ctx, tx, r.SourceAccountID); e != nil {
		return 0, e
	}
	var state string
	e = tx.QueryRow(ctx, `SELECT state FROM account_reservations WHERE transfer_id=$1`, r.ID).Scan(&state)
	if e == nil {
		events, e := loadEventsTx(ctx, tx, r.SourceAccountID)
		if e != nil {
			return 0, e
		}
		return domain.ReplayAccount(r.SourceAccountID, events).Balance, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return 0, e
	}
	src, e := loadEventsTx(ctx, tx, r.SourceAccountID)
	if e != nil {
		return 0, e
	}
	dst, e := loadEventsTx(ctx, tx, r.DestinationAccountID)
	if e != nil {
		return 0, e
	}
	if len(src) == 0 || len(dst) == 0 {
		return 0, domain.ErrAccountNotFound
	}
	a := domain.ReplayAccount(r.SourceAccountID, src)
	amount := r.Amount + r.Fee
	if a.AvailableBalance < amount {
		return 0, domain.NewInsufficientFundsError(a, amount)
	}
	if _, e = tx.Exec(ctx, `INSERT INTO account_reservations(transfer_id,account_id,amount_minor,state,created_at,updated_at) VALUES($1,$2,$3,'reserved',$4,$4)`, r.ID, r.SourceAccountID, int64(amount), at); e != nil {
		return 0, e
	}
	ev := domain.FundsReserved{Aggregate: r.SourceAccountID, Type: "FundsReserved", Seq: len(src), Occurred: at, ID: r.SourceAccountID, Amount: amount, TransferID: r.ID}
	if e = insertEventTx(ctx, tx, ev); e != nil {
		return 0, e
	}
	if e = tx.Commit(ctx); e != nil {
		return 0, e
	}
	return a.Balance, nil
}
func (s *PostgresStore) RecordComplianceDecision(id, decision string, at time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, e := s.pool.Exec(ctx, `INSERT INTO compliance_decisions(transfer_id,decision,decided_at) VALUES($1,$2,$3) ON CONFLICT(transfer_id) DO UPDATE SET decision=EXCLUDED.decision WHERE compliance_decisions.decision=EXCLUDED.decision`, id, decision, at)
	if e == nil && tag.RowsAffected() == 0 {
		return domain.ErrIdempotencyConflict
	}
	return e
}
func (s *PostgresStore) PostTransferLedger(r domain.TransferRecord, at time.Time) (domain.Money, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(ctx)
	for _, id := range []string{minString(r.SourceAccountID, r.DestinationAccountID), maxString(r.SourceAccountID, r.DestinationAccountID)} {
		if e = lockAccountTx(ctx, tx, id); e != nil {
			return 0, e
		}
	}
	var exists bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ledger_postings WHERE transfer_id=$1)`, r.ID).Scan(&exists); e != nil {
		return 0, e
	}
	if exists {
		events, e := loadEventsTx(ctx, tx, r.SourceAccountID)
		if e != nil {
			return 0, e
		}
		return domain.ReplayAccount(r.SourceAccountID, events).Balance, nil
	}
	src, e := loadEventsTx(ctx, tx, r.SourceAccountID)
	if e != nil {
		return 0, e
	}
	dst, e := loadEventsTx(ctx, tx, r.DestinationAccountID)
	if e != nil {
		return 0, e
	}
	if r.Fee > 0 {
		ev := domain.MoneyDebited{Aggregate: r.SourceAccountID, Type: "MoneyDebited", Seq: len(src), Occurred: at, ID: r.SourceAccountID, Amount: r.Fee, TransferID: r.ID, Purpose: "fee", CounterpartyAccountID: r.DestinationAccountID}
		if e = insertEventTx(ctx, tx, ev); e != nil {
			return 0, e
		}
		src = append(src, ev)
	}
	debit := domain.MoneyDebited{Aggregate: r.SourceAccountID, Type: "MoneyDebited", Seq: len(src), Occurred: at, ID: r.SourceAccountID, Amount: r.Amount, TransferID: r.ID, Purpose: "transfer", CounterpartyAccountID: r.DestinationAccountID}
	credit := domain.MoneyCredited{Aggregate: r.DestinationAccountID, Type: "MoneyCredited", Seq: len(dst), Occurred: at, ID: r.DestinationAccountID, Amount: r.Amount, TransferID: r.ID, Purpose: "transfer", CounterpartyAccountID: r.SourceAccountID}
	if e = insertEventTx(ctx, tx, debit); e != nil {
		return 0, e
	}
	if e = insertEventTx(ctx, tx, credit); e != nil {
		return 0, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO ledger_postings(transfer_id,posted_at) VALUES($1,$2)`, r.ID, at); e != nil {
		return 0, e
	}
	payload, _ := json.Marshal(domain.TransferCompletedEvent{TransferID: r.ID, Timestamp: at, Amount: r.Amount, SourceAccountID: r.SourceAccountID, DestinationAccountID: r.DestinationAccountID, Fee: r.Fee})
	if e = s.appendIntegrationEventTx(ctx, tx, domain.IntegrationEvent{TransferID: r.ID, EventType: "TransferCompleted", Payload: payload, Status: domain.IntegrationEventScheduled, NextAttemptAt: at, CreatedAt: at}); e != nil {
		return 0, e
	}
	if e = tx.Commit(ctx); e != nil {
		return 0, e
	}
	src = append(src, debit)
	return domain.ReplayAccount(r.SourceAccountID, src).Balance, nil
}
func (s *PostgresStore) CaptureTransferReservation(r domain.TransferRecord, at time.Time) error {
	return s.finishTransferReservation(r, at, "captured")
}
func (s *PostgresStore) ReleaseTransferReservation(r domain.TransferRecord, at time.Time) error {
	return s.finishTransferReservation(r, at, "released")
}
func (s *PostgresStore) finishTransferReservation(r domain.TransferRecord, at time.Time, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = lockAccountTx(ctx, tx, r.SourceAccountID); e != nil {
		return e
	}
	var account, state string
	var amount domain.Money
	e = tx.QueryRow(ctx, `SELECT account_id,amount_minor,state FROM account_reservations WHERE transfer_id=$1 FOR UPDATE`, r.ID).Scan(&account, &amount, &state)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if state == target {
		return nil
	}
	if state != "reserved" {
		return domain.ErrIdempotencyConflict
	}
	events, e := loadEventsTx(ctx, tx, account)
	if e != nil {
		return e
	}
	var ev domain.Event
	if target == "captured" {
		ev = domain.ReservationCaptured{Aggregate: account, Type: "ReservationCaptured", Seq: len(events), Occurred: at, ID: account, Amount: amount, TransferID: r.ID}
	} else {
		ev = domain.ReservationReleased{Aggregate: account, Type: "ReservationReleased", Seq: len(events), Occurred: at, ID: account, Amount: amount, TransferID: r.ID}
	}
	if e = insertEventTx(ctx, tx, ev); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE account_reservations SET state=$2,updated_at=$3 WHERE transfer_id=$1`, r.ID, target, at); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) WithdrawAvailable(accountID string, amount domain.Money, at time.Time) (*domain.Account, *domain.Account, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, nil, e
	}
	defer tx.Rollback(ctx)
	if e = lockAccountTx(ctx, tx, accountID); e != nil {
		return nil, nil, e
	}
	events, e := loadEventsTx(ctx, tx, accountID)
	if e != nil {
		return nil, nil, e
	}
	if len(events) == 0 {
		return nil, nil, domain.ErrAccountNotFound
	}
	initial := domain.ReplayAccount(accountID, events)
	if initial.AvailableBalance < amount {
		return nil, nil, domain.NewInsufficientFundsError(initial, amount)
	}
	ev := domain.MoneyDebited{Aggregate: accountID, Type: "MoneyDebited", Seq: len(events), Occurred: at, ID: accountID, Amount: amount}
	if e = insertEventTx(ctx, tx, ev); e != nil {
		return nil, nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, nil, e
	}
	return initial, domain.ReplayAccount(accountID, append(events, ev)), nil
}

// execer is implemented by both pgx.Tx and *pgxpool.Pool, so the outbox insert
// works inside a transaction (atomic with the ledger post) or standalone.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// appendIntegrationEventTx enqueues a durable outbox row inside a transaction so
// the row commits atomically with the ledger posting. It is idempotent on
// (transfer_id, event_type) via the unique index, so a retried ledger post never
// enqueues the event twice.
func (s *PostgresStore) appendIntegrationEventTx(ctx context.Context, tx execer, event domain.IntegrationEvent) error {
	payload := event.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO integration_events (transfer_id, event_type, payload, status, next_attempt_at, created_at)
		VALUES ($1, $2, $3, 'scheduled', $4, $5)
		ON CONFLICT (transfer_id, event_type) DO NOTHING
	`, event.TransferID, event.EventType, payload, event.NextAttemptAt, event.CreatedAt)
	return err
}

func (s *PostgresStore) AppendIntegrationEvent(event domain.IntegrationEvent) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if event.NextAttemptAt.IsZero() {
		event.NextAttemptAt = event.CreatedAt
	}
	return s.appendIntegrationEventTx(ctx, s.pool, event)
}

func scanIntegrationEvent(row pgx.Row) (*domain.IntegrationEvent, error) {
	var e domain.IntegrationEvent
	var payload []byte
	err := row.Scan(&e.ID, &e.TransferID, &e.EventType, &payload, &e.Status, &e.AttemptCount, &e.NextAttemptAt, &e.LastError, &e.PublishedAt, &e.CreatedAt)
	e.Payload = payload
	return &e, err
}

const integrationEventColumns = `id,transfer_id,event_type,payload,status,attempt_count,next_attempt_at,last_error,published_at,created_at`

func (s *PostgresStore) ListDueIntegrationEvents(now time.Time, limit int) ([]domain.IntegrationEvent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.pool.Query(ctx, `SELECT `+integrationEventColumns+` FROM integration_events WHERE status='scheduled' AND next_attempt_at<=$1 ORDER BY id LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.IntegrationEvent{}
	for rows.Next() {
		member, e := scanIntegrationEvent(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *member)
	}
	return out, rows.Err()
}

func (s *PostgresStore) MarkIntegrationEventPublished(id int64, at time.Time) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, err := s.pool.Exec(ctx, `UPDATE integration_events SET status='published', published_at=$2 WHERE id=$1 AND status='scheduled'`, id, at)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *PostgresStore) RecordIntegrationEventFailure(event domain.IntegrationEvent, nextAttemptAt time.Time, deadLetter bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.pool.Exec(ctx, `
		UPDATE integration_events SET
			status = CASE WHEN $2 THEN 'dead_lettered' ELSE status END,
			attempt_count = $3,
			last_error = $4,
			next_attempt_at = CASE WHEN $2 THEN next_attempt_at ELSE $5 END
		WHERE id = $1 AND status = 'scheduled'
	`, event.ID, deadLetter, event.AttemptCount, event.LastError, nextAttemptAt)
	return err
}

func (s *PostgresStore) RequeueIntegrationEvent(id int64, at time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.pool.Exec(ctx, `UPDATE integration_events SET status='scheduled', attempt_count=0, last_error='', next_attempt_at=$2, published_at=NULL WHERE id=$1 AND status='dead_lettered'`, id, at)
	return err
}

func (s *PostgresStore) ListIntegrationEvents(status string, limit int) ([]domain.IntegrationEvent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	query := `SELECT ` + integrationEventColumns + ` FROM integration_events WHERE $1='' OR status=$1 ORDER BY id DESC`
	args := []any{status}
	if limit > 0 {
		query += ` LIMIT $2`
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.IntegrationEvent{}
	for rows.Next() {
		member, e := scanIntegrationEvent(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *member)
	}
	return out, rows.Err()
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
	return scanTransfer(s.pool.QueryRow(ctx, `SELECT `+transferColumns+` FROM transfers WHERE source_account_id=$1 AND idempotency_key=$2`, source, key))
}

const transferColumns = `id,source_account_id,destination_account_id,idempotency_key,amount_minor,fee_minor,status,failure_code,initial_source_balance_minor,final_source_balance_minor,created_at,updated_at,current_step,compliance_status,attempt_count,next_attempt_at,last_error`

func scanTransfer(row pgx.Row) (*domain.TransferRecord, error) {
	var r domain.TransferRecord
	e := row.Scan(&r.ID, &r.SourceAccountID, &r.DestinationAccountID, &r.IdempotencyKey, &r.Amount, &r.Fee, &r.Status, &r.FailureCode, &r.InitialSourceBalance, &r.FinalSourceBalance, &r.CreatedAt, &r.UpdatedAt, &r.CurrentStep, &r.ComplianceStatus, &r.AttemptCount, &r.NextAttemptAt, &r.LastError)
	return &r, e
}
func (s *PostgresStore) FindTransfer(id string) (*domain.TransferRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := scanTransfer(s.pool.QueryRow(ctx, `SELECT `+transferColumns+` FROM transfers WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrTransferNotFound
	}
	return r, err
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

// SaveSnapshot upserts the latest account snapshot for an aggregate. A snapshot
// whose AsOfSequence is not newer than the stored one is ignored, so a slow
// writer can't overwrite a newer snapshot.
func (s *PostgresStore) SaveSnapshot(snap domain.AccountSnapshot) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.pool.Exec(ctx, `
		INSERT INTO account_snapshots
			(aggregate_id, balance_minor, reserved_balance_minor, available_balance_minor, as_of_sequence, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (aggregate_id) DO UPDATE
		SET balance_minor=EXCLUDED.balance_minor,
		    reserved_balance_minor=EXCLUDED.reserved_balance_minor,
		    available_balance_minor=EXCLUDED.available_balance_minor,
		    as_of_sequence=EXCLUDED.as_of_sequence,
		    occurred_at=EXCLUDED.occurred_at
		WHERE account_snapshots.as_of_sequence < EXCLUDED.as_of_sequence
	`, snap.AggregateID, int64(snap.Balance), int64(snap.ReservedBalance), int64(snap.AvailableBalance), int64(snap.AsOfSequence), snap.OccurredAt)
	if err != nil {
		return fmt.Errorf("eventstore: save snapshot: %w", err)
	}
	return nil
}

// LoadSnapshot returns the latest account snapshot, or nil and ErrNoSnapshot.
func (s *PostgresStore) LoadSnapshot(aggregateID string) (*domain.AccountSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var snap domain.AccountSnapshot
	var balance, reserved, available int64
	err := s.pool.QueryRow(ctx, `
		SELECT balance_minor, reserved_balance_minor, available_balance_minor, as_of_sequence, occurred_at
		FROM account_snapshots
		WHERE aggregate_id = $1
	`, aggregateID).Scan(&balance, &reserved, &available, &snap.AsOfSequence, &snap.OccurredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSnapshot
	}
	if err != nil {
		return nil, fmt.Errorf("eventstore: load snapshot: %w", err)
	}
	snap.AggregateID = aggregateID
	snap.Balance = domain.Money(balance)
	snap.ReservedBalance = domain.Money(reserved)
	snap.AvailableBalance = domain.Money(available)
	return &snap, nil
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
	case "FundsReserved":
		var e domain.FundsReserved
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, fmt.Errorf("eventstore: decode FundsReserved: %w", err)
		}
		e.Aggregate = aggregateID
		return e, nil
	case "ReservationCaptured":
		var e domain.ReservationCaptured
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, fmt.Errorf("eventstore: decode ReservationCaptured: %w", err)
		}
		e.Aggregate = aggregateID
		return e, nil
	case "ReservationReleased":
		var e domain.ReservationReleased
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, fmt.Errorf("eventstore: decode ReservationReleased: %w", err)
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

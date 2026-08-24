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

func (s *PostgresStore) CreateCustomerAccount(customer domain.Customer, opened domain.AccountOpened) error {
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
			passport_image_mime, kyc_status, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
	`, customer.ID, customer.AccountID, customer.LegalFirstName, customer.LegalLastName,
		customer.DateOfBirth, customer.Nationality, customer.Email, customer.Phone, customer.PasswordHash,
		customer.ResidentialAddress.Line1, customer.ResidentialAddress.Line2,
		customer.ResidentialAddress.City, customer.ResidentialAddress.StateOrProvince,
		customer.ResidentialAddress.PostalCode, customer.ResidentialAddress.Country,
		customer.GovernmentDocument.Type, customer.GovernmentDocument.Number,
		customer.GovernmentDocument.IssuingCountry, customer.PassportImage,
		customer.PassportImageMIME, customer.KYCStatus, customer.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrCustomerAlreadyExists
		}
		return fmt.Errorf("eventstore: insert customer: %w", err)
	}
	payload, err := json.Marshal(opened)
	if err != nil {
		return fmt.Errorf("eventstore: marshal opening event: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO events (aggregate_id, sequence, event_type, payload, occurred_at)
		VALUES ($1, 0, $2, $3, $4)
	`, opened.AggregateID(), opened.EventType(), payload, opened.OccurredAt())
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrConcurrencyConflict
		}
		return fmt.Errorf("eventstore: insert opening event: %w", err)
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
	err := s.pool.QueryRow(ctx, `SELECT id, account_id, email, password_hash FROM customers WHERE lower(email)=lower($1)`, email).
		Scan(&customer.ID, &customer.AccountID, &customer.Email, &customer.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("eventstore: find customer: %w", err)
	}
	return &customer, nil
}

func (s *PostgresStore) CreateSession(session domain.Session) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.pool.Exec(ctx, `INSERT INTO sessions (token_hash, customer_id, account_id, expires_at) VALUES ($1,$2,$3,$4)`, session.TokenHash, session.CustomerID, session.AccountID, session.ExpiresAt)
	if err != nil {
		return fmt.Errorf("eventstore: create session: %w", err)
	}
	return nil
}

func (s *PostgresStore) FindSession(tokenHash string, now time.Time) (*domain.Principal, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var principal domain.Principal
	err := s.pool.QueryRow(ctx, `SELECT s.customer_id, s.account_id, c.email FROM sessions s JOIN customers c ON c.id=s.customer_id WHERE s.token_hash=$1 AND s.expires_at>$2`, tokenHash, now).
		Scan(&principal.CustomerID, &principal.AccountID, &principal.Email)
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

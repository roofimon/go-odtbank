package eventstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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

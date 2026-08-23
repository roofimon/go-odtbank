package eventstore

import (
	"errors"

	"go-odtbank/internal/domain"
)

// Store is the persistence contract for the append-only event stream.
// A Postgres-backed implementation can satisfy this interface without changes
// to the service or domain layers.
type Store interface {
	// Append persists a new event for an aggregate. The caller's expected
	// version is the version the aggregate was loaded at; if it does not match
	// the store's current version, ErrConcurrencyConflict is returned and the
	// event is NOT appended.
	Append(event domain.Event, expectedVersion int) error

	// Load returns every event recorded for the given aggregate, in append
	// order. An aggregate with no events yields an empty slice.
	Load(aggregateID string) ([]domain.Event, error)

	// ListAggregates returns the IDs of all aggregates that have at least one
	// event, sorted ascending.
	ListAggregates() ([]string, error)
}

var ErrConcurrencyConflict = errors.New("event store: concurrency conflict on append")

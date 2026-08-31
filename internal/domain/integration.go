package domain

import (
	"errors"
	"time"
)

const (
	IntegrationEventScheduled  = "scheduled"
	IntegrationEventPublished  = "published"
	IntegrationEventDeadLetter = "dead_lettered"
)

// IntegrationEvent is a durable outbox row. It records an integration-level fact
// (today, a completed transfer) that must reach downstream consumers even if the
// process crashes. The row is appended in the same transaction as the ledger
// posting that produced it, so a crash after the money moves but before delivery
// leaves a "scheduled" row the durable worker redelivers. The worker advances the
// row to "published" on success or "dead_lettered" after retries are exhausted.
type IntegrationEvent struct {
	ID            int64      `json:"id"`
	TransferID    string     `json:"transfer_id"`
	EventType     string     `json:"event_type"`
	Payload       []byte     `json:"-"`
	Status        string     `json:"status"`
	AttemptCount  int        `json:"attempt_count,omitempty"`
	NextAttemptAt time.Time  `json:"next_attempt_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// EventBusStore is the durable outbox contract. A store that also appends account
// events calls AppendIntegrationEvent inside the ledger-post transaction so the
// outbox row is committed atomically with the money movement; the durable worker
// then drains ListDue rows and delivers them to subscribers.
type EventBusStore interface {
	// AppendIntegrationEvent enqueues one durable outbox row. It is idempotent on
	// TransferID: a second call for the same transfer is a no-op, so a retried
	// ledger post never enqueues the event twice.
	AppendIntegrationEvent(event IntegrationEvent) error

	// ListDueIntegrationEvents returns "scheduled" rows whose next_attempt_at is at
	// or before now, oldest first, up to limit.
	ListDueIntegrationEvents(now time.Time, limit int) ([]IntegrationEvent, error)

	// MarkIntegrationEventPublished atomically moves a row to "published" only if it
	// is still "scheduled", recording published_at. It returns false when the row is
	// absent or already advanced, so a concurrent worker cannot double-publish.
	MarkIntegrationEventPublished(id int64, at time.Time) (bool, error)

	// RecordIntegrationEventFailure atomically records a failed delivery: it
	// increments attempt_count, stores last_error, and either dead-letters the row or
	// schedules the next attempt. It only applies to "scheduled" rows.
	RecordIntegrationEventFailure(event IntegrationEvent, nextAttemptAt time.Time, deadLetter bool) error

	// RequeueIntegrationEvent returns a dead-lettered row to "scheduled" for a manual
	// retry, resetting attempt_count and last_error.
	RequeueIntegrationEvent(id int64, at time.Time) error

	// ListIntegrationEvents returns outbox rows by status ("", or any status, or a
	// specific status), newest first, up to limit.
	ListIntegrationEvents(status string, limit int) ([]IntegrationEvent, error)
}

var (
	ErrIntegrationEventNotFound = errors.New("integration event not found")
)

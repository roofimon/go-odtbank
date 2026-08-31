package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"go-odtbank/internal/domain"
)

var errNoRecipient = errors.New("no subscriber registered for event type")

// Subscriber receives a decoded integration event. It runs inside the durable
// worker's panic recovery, so a panicking subscriber is recorded as a failed
// delivery rather than crashing the process. Returning an error (or panicking)
// schedules a retry.
type Subscriber func(ctx context.Context, event domain.TransferCompletedEvent) error

type Options struct {
	// Interval between due-scan passes when no new work is signalled.
	Interval time.Duration
	// MaxAttempts is the number of delivery attempts before a row is dead-lettered.
	MaxAttempts int
	// MaxBackoff caps the exponential retry delay between attempts.
	MaxBackoff time.Duration
}

func DefaultOptions() Options {
	return Options{Interval: 250 * time.Millisecond, MaxAttempts: 5, MaxBackoff: 30 * time.Second}
}

// DurableBus is a durable, retrying publisher for integration events. Its
// outbox rows are appended by the store in the same transaction as the money
// movement that produced them, so a crash after the ledger post but before
// delivery leaves a "scheduled" row that this worker redelivers. Failed
// deliveries are retried with exponential backoff and dead-lettered after
// MaxAttempts, instead of being lost as in a fire-and-forget publisher.
type DurableBus struct {
	store domain.EventBusStore
	sub   map[string][]Subscriber
	mu    sync.RWMutex
	opts  Options
	wake  chan struct{}
}

func NewDurableBus(store domain.EventBusStore, opts Options) *DurableBus {
	if opts.Interval <= 0 {
		opts.Interval = 250 * time.Millisecond
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 5
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 30 * time.Second
	}
	return &DurableBus{
		store: store,
		sub:   map[string][]Subscriber{},
		opts:  opts,
		wake:  make(chan struct{}, 1),
	}
}

// Register subscribes to a specific event type.
func (b *DurableBus) Register(eventType string, s Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sub[eventType] = append(b.sub[eventType], s)
}

// Wake signals the worker to scan the outbox immediately. Non-blocking.
func (b *DurableBus) Wake() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// Run drives the outbox until ctx is cancelled.
func (b *DurableBus) Run(ctx context.Context) {
	ticker := time.NewTicker(b.opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-b.wake:
		}
		if err := b.ProcessDue(ctx); err != nil {
			log.Printf("[DurableBus] process due: %v", err)
		}
	}
}

// ProcessDue drains one batch of scheduled, due outbox rows.
func (b *DurableBus) ProcessDue(ctx context.Context) error {
	rows, err := b.store.ListDueIntegrationEvents(time.Now().UTC(), 100)
	if err != nil {
		return err
	}
	for i := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		b.process(&rows[i])
	}
	return nil
}

// process delivers one outbox row to its subscribers, marking it published on
// success or scheduling a retry / dead-letter on failure.
func (b *DurableBus) process(event *domain.IntegrationEvent) {
	delivered, err := b.deliver(ctxBackground(), *event)
	if err != nil {
		b.recordFailure(*event, err)
		return
	}
	if delivered {
		if _, markErr := b.store.MarkIntegrationEventPublished(event.ID, time.Now().UTC()); markErr != nil {
			log.Printf("[DurableBus] mark published id=%d: %v", event.ID, markErr)
		}
		log.Printf("[DurableBus] published transfer %s (id=%d)", event.TransferID, event.ID)
		return
	}
	b.recordFailure(*event, errNoRecipient)
}

// deliver decodes the row payload and dispatches to subscribers with panic
// recovery. delivered is true when at least one subscriber handled the event.
func (b *DurableBus) deliver(ctx context.Context, event domain.IntegrationEvent) (bool, error) {
	var tc domain.TransferCompletedEvent
	if len(event.Payload) > 0 {
		if err := json.Unmarshal(event.Payload, &tc); err != nil {
			return false, err
		}
	}
	subs := b.subscribers(event.EventType)
	if len(subs) == 0 {
		return false, errNoRecipient
	}
	for _, s := range subs {
		if err := b.invoke(ctx, s, tc); err != nil {
			return len(subs) > 0, err
		}
	}
	return true, nil
}

func (b *DurableBus) subscribers(eventType string) []Subscriber {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Subscriber, len(b.sub[eventType]))
	copy(out, b.sub[eventType])
	return out
}

// invoke runs a subscriber with panic recovery, converting a panic into an error.
func (b *DurableBus) invoke(ctx context.Context, s Subscriber, event domain.TransferCompletedEvent) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[DurableBus] recovered panic delivering to subscriber: %v", r)
			err = fmt.Errorf("subscriber panic: %v", r)
		}
	}()
	return s(ctx, event)
}

func (b *DurableBus) recordFailure(event domain.IntegrationEvent, cause error) {
	attempt := event.AttemptCount + 1
	deadLetter := attempt >= b.opts.MaxAttempts
	msg := cause.Error()
	if deadLetter {
		log.Printf("[DurableBus] dead-lettering transfer %s (id=%d) after %d attempts: %v", event.TransferID, event.ID, attempt, cause)
	} else {
		log.Printf("[DurableBus] delivery failed for transfer %s (id=%d), attempt %d: %v", event.TransferID, event.ID, attempt, cause)
	}
	backoff := time.Duration(1) << min(attempt-1, 5)
	if backoff > b.opts.MaxBackoff {
		backoff = b.opts.MaxBackoff
	}
	_ = b.store.RecordIntegrationEventFailure(domain.IntegrationEvent{ID: event.ID, AttemptCount: attempt, LastError: msg}, time.Now().UTC().Add(backoff), deadLetter)
}

func ctxBackground() context.Context { return context.Background() }

package eventbus_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventbus"
	"go-odtbank/internal/eventstore"
)

// enqueue appends one scheduled outbox row, decoded from a TransferCompletedEvent,
// due immediately so ProcessDue will deliver it this pass.
func enqueue(t *testing.T, store *eventstore.MemoryStore, transferID string, tc domain.TransferCompletedEvent) {
	t.Helper()
	payload, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	tc.TransferID = transferID
	if err := store.AppendIntegrationEvent(domain.IntegrationEvent{
		TransferID:    transferID,
		EventType:     "TransferCompleted",
		Payload:       payload,
		Status:        domain.IntegrationEventScheduled,
		NextAttemptAt: time.Now().UTC().Add(-time.Second),
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
}

func rowByTransfer(t *testing.T, store *eventstore.MemoryStore, transferID string) domain.IntegrationEvent {
	t.Helper()
	rows, err := store.ListIntegrationEvents("", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.TransferID == transferID {
			return r
		}
	}
	t.Fatalf("no outbox row for transfer %s", transferID)
	return domain.IntegrationEvent{}
}

// A successfully delivered outbox row is marked published exactly once.
func TestDurableBus_DeliversAndPublishes(t *testing.T) {
	store := eventstore.NewMemoryStore()
	var mu sync.Mutex
	var got []domain.TransferCompletedEvent
	bus := eventbus.NewDurableBus(store, eventbus.Options{Interval: time.Millisecond, MaxAttempts: 5})
	bus.Register("TransferCompleted", func(ctx context.Context, e domain.TransferCompletedEvent) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e)
		return nil
	})

	enqueue(t, store, "trf_1", domain.TransferCompletedEvent{TransferID: "trf_1", Amount: 1000, SourceAccountID: "a", DestinationAccountID: "b"})
	if err := bus.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].Amount != 1000 || got[0].SourceAccountID != "a" || got[0].DestinationAccountID != "b" {
		t.Fatalf("delivered=%+v, want one 1000 a->b", got)
	}
	row := rowByTransfer(t, store, "trf_1")
	if row.Status != domain.IntegrationEventPublished {
		t.Fatalf("status=%q, want published", row.Status)
	}
	if row.PublishedAt == nil {
		t.Fatal("published_at not set")
	}
}

// A failing subscriber is retried and dead-lettered after MaxAttempts. Between
// passes the test forces the row due again, so exhaustion is driven by attempt
// count rather than wall-clock backoff resolution.
func TestDurableBus_RetriesThenDeadLetters(t *testing.T) {
	store := eventstore.NewMemoryStore()
	failCount := 0
	bus := eventbus.NewDurableBus(store, eventbus.Options{Interval: time.Millisecond, MaxAttempts: 3, MaxBackoff: time.Millisecond})
	bus.Register("TransferCompleted", func(ctx context.Context, e domain.TransferCompletedEvent) error {
		failCount++
		return errors.New("downstream unavailable")
	})

	enqueue(t, store, "trf_2", domain.TransferCompletedEvent{TransferID: "trf_2", Amount: 500})
	for i := 0; i < 5; i++ {
		row := rowByTransfer(t, store, "trf_2")
		if row.Status == domain.IntegrationEventDeadLetter {
			break
		}
		if err := bus.ProcessDue(context.Background()); err != nil {
			t.Fatal(err)
		}
		// Keep the row due so the next pass re-delivers it regardless of how the
		// worker scheduled its backoff relative to the wall clock.
		current := rowByTransfer(t, store, "trf_2")
		if current.Status == domain.IntegrationEventScheduled {
			if err := store.RecordIntegrationEventFailure(current, time.Now().UTC().Add(-time.Hour), false); err != nil {
				t.Fatal(err)
			}
		}
	}
	row := rowByTransfer(t, store, "trf_2")
	if row.Status != domain.IntegrationEventDeadLetter {
		t.Fatalf("status=%q, want dead_lettered", row.Status)
	}
	if failCount < 3 {
		t.Fatalf("delivery attempts=%d, want >=3", failCount)
	}
}

// A panicking subscriber is recovered and recorded as a failed delivery, not lost.
func TestDurableBus_RecoversPanic(t *testing.T) {
	store := eventstore.NewMemoryStore()
	bus := eventbus.NewDurableBus(store, eventbus.Options{Interval: time.Millisecond, MaxAttempts: 5})
	bus.Register("TransferCompleted", func(ctx context.Context, e domain.TransferCompletedEvent) error {
		panic("boom")
	})

	enqueue(t, store, "trf_3", domain.TransferCompletedEvent{TransferID: "trf_3", Amount: 750})
	if err := bus.ProcessDue(context.Background()); err != nil {
		t.Fatalf("ProcessDue panicked or errored: %v", err)
	}
	row := rowByTransfer(t, store, "trf_3")
	if row.Status == domain.IntegrationEventPublished {
		t.Fatal("panicking subscriber must not mark the row published")
	}
	if row.AttemptCount != 1 || row.LastError == "" {
		t.Fatalf("row=%+v, want a recorded failure", row)
	}
}

// A dead-lettered row can be requeued and then delivered successfully.
func TestDurableBus_RequeueAndDeliver(t *testing.T) {
	store := eventstore.NewMemoryStore()
	enqueue(t, store, "trf_4", domain.TransferCompletedEvent{TransferID: "trf_4", Amount: 300})
	row := rowByTransfer(t, store, "trf_4")
	if err := store.RecordIntegrationEventFailure(row, time.Now().UTC().Add(time.Hour), true); err != nil {
		t.Fatal(err)
	}
	if got := rowByTransfer(t, store, "trf_4"); got.Status != domain.IntegrationEventDeadLetter {
		t.Fatalf("status=%q, want dead_lettered", got.Status)
	}

	delivered := make(chan struct{}, 1)
	bus := eventbus.NewDurableBus(store, eventbus.Options{Interval: time.Millisecond, MaxAttempts: 5})
	bus.Register("TransferCompleted", func(ctx context.Context, e domain.TransferCompletedEvent) error {
		select {
		case delivered <- struct{}{}:
		default:
		}
		return nil
	})

	if err := store.RequeueIntegrationEvent(row.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := bus.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-delivered:
	default:
		t.Fatal("requeued event was not delivered")
	}
	if got := rowByTransfer(t, store, "trf_4"); got.Status != domain.IntegrationEventPublished {
		t.Fatalf("status=%q, want published", got.Status)
	}
}

// No registered subscriber keeps the row scheduled so it is retryable later.
func TestDurableBus_NoSubscriberKeepsScheduled(t *testing.T) {
	store := eventstore.NewMemoryStore()
	bus := eventbus.NewDurableBus(store, eventbus.Options{Interval: time.Millisecond, MaxAttempts: 3})
	enqueue(t, store, "trf_5", domain.TransferCompletedEvent{TransferID: "trf_5", Amount: 100})
	if err := bus.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rowByTransfer(t, store, "trf_5"); got.Status != domain.IntegrationEventScheduled {
		t.Fatalf("status=%q, want scheduled (no subscriber)", got.Status)
	}
}

// Appending the same transfer's event twice must enqueue only one outbox row.
func TestDurableBus_IdempotentEnqueue(t *testing.T) {
	store := eventstore.NewMemoryStore()
	enqueue(t, store, "trf_6", domain.TransferCompletedEvent{TransferID: "trf_6", Amount: 1})
	if err := store.AppendIntegrationEvent(domain.IntegrationEvent{
		TransferID: "trf_6", EventType: "TransferCompleted", Payload: []byte("{}"),
		Status: domain.IntegrationEventScheduled, NextAttemptAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	rows, _ := store.ListIntegrationEvents("", 0)
	if len(rows) != 1 {
		t.Fatalf("outbox rows=%d, want 1 (idempotent)", len(rows))
	}
}

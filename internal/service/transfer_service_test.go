package service_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/policy"
	"go-odtbank/internal/service"
)

type capturedEvent struct {
	got domain.TransferCompletedEvent
}

func TestTransfer_IdempotentRetryAndConflict(t *testing.T) {
	store := eventstore.NewMemoryStore()
	for _, item := range []struct {
		id      string
		balance domain.Money
	}{{"a", 10000}, {"b", 0}} {
		_ = store.Append(domain.AccountOpened{Aggregate: item.id, Type: "AccountOpened", ID: item.id, InitialBalance: item.balance, Occurred: time.Now()}, 0)
	}
	published := 0
	svc := service.NewTransferService(store, &policy.ZeroFeePolicy{}, &policy.DefaultTimeService{ServiceAvailable: true}, func(domain.TransferCompletedEvent) { published++ })
	cmd := domain.TransferCommand{Amount: 1000, SourceAccountID: "a", DestinationAccountID: "b", IdempotencyKey: "same"}
	first, err := svc.Transfer(cmd)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Transfer(cmd)
	if err != nil || second.TransferID != first.TransferID {
		t.Fatalf("retry=%+v err=%v", second, err)
	}
	if published != 1 {
		t.Fatalf("published=%d", published)
	}
	cmd.Amount = 2000
	if _, err = svc.Transfer(cmd); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("conflict=%v", err)
	}
	events, _ := store.Load("a")
	if len(events) != 2 {
		t.Fatalf("source events=%d", len(events))
	}
}

func TestTransfer_ConcurrentDuplicateExecutesOnce(t *testing.T) {
	store := eventstore.NewMemoryStore()
	_ = store.Append(domain.AccountOpened{Aggregate: "a", Type: "AccountOpened", ID: "a", InitialBalance: 10000, Occurred: time.Now()}, 0)
	_ = store.Append(domain.AccountOpened{Aggregate: "b", Type: "AccountOpened", ID: "b", Occurred: time.Now()}, 0)
	svc := service.NewTransferService(store, &policy.ZeroFeePolicy{}, &policy.DefaultTimeService{ServiceAvailable: true}, nil)
	cmd := domain.TransferCommand{Amount: 1000, SourceAccountID: "a", DestinationAccountID: "b", IdempotencyKey: "concurrent"}
	var wg sync.WaitGroup
	ids := make(chan string, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := svc.Transfer(cmd)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- r.TransferID
		}()
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		} else if id != first {
			t.Errorf("ids differ %s %s", first, id)
		}
	}
	events, _ := store.Load("a")
	if len(events) != 2 {
		t.Fatalf("source events=%d", len(events))
	}
}

type failingAtomicStore struct{ base *eventstore.MemoryStore }

func (f failingAtomicStore) ExecuteTransfer(domain.TransferRecord) (*domain.TransferRecord, bool, error) {
	return nil, false, errors.New("simulated credit failure")
}
func (f failingAtomicStore) FindTransfer(string) (*domain.TransferRecord, error) {
	return nil, domain.ErrTransferNotFound
}
func TestTransfer_CreditFailureChangesNeitherStream(t *testing.T) {
	base := eventstore.NewMemoryStore()
	_ = base.Append(domain.AccountOpened{Aggregate: "a", Type: "AccountOpened", ID: "a", InitialBalance: 10000, Occurred: time.Now()}, 0)
	_ = base.Append(domain.AccountOpened{Aggregate: "b", Type: "AccountOpened", ID: "b", Occurred: time.Now()}, 0)
	svc := service.NewTransferService(failingAtomicStore{base}, &policy.ZeroFeePolicy{}, &policy.DefaultTimeService{ServiceAvailable: true}, nil)
	_, _ = svc.Transfer(domain.TransferCommand{Amount: 1000, SourceAccountID: "a", DestinationAccountID: "b", IdempotencyKey: "failure"})
	a, _ := base.Load("a")
	b, _ := base.Load("b")
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("events changed: %d %d", len(a), len(b))
	}
}

func TestTransfer_AppendsEventsAndReplaysState(t *testing.T) {
	store := eventstore.NewMemoryStore()

	seed := func(id string, balance domain.Money) {
		if err := store.Append(domain.AccountOpened{
			Aggregate:      id,
			Type:           "AccountOpened",
			Seq:            0,
			Occurred:       time.Now(),
			ID:             id,
			InitialBalance: balance,
		}, 0); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed("acc1", 10000)
	seed("acc2", 5000)

	var captured capturedEvent
	bus := func(ev domain.TransferCompletedEvent) {
		captured.got = ev
	}

	svc := service.NewTransferService(store, &policy.ZeroFeePolicy{}, &policy.DefaultTimeService{ServiceAvailable: true}, bus)

	receipt, err := svc.Transfer(domain.TransferCommand{Amount: 2500, SourceAccountID: "acc1", DestinationAccountID: "acc2", IdempotencyKey: "test-key"})
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}

	if got, want := receipt.InitialSourceAccount.Balance, domain.Money(10000); got != want {
		t.Errorf("initial source balance = %v, want %v", got, want)
	}
	if got, want := receipt.FinalSourceAccount.Balance, domain.Money(7500); got != want {
		t.Errorf("final source balance = %v, want %v", got, want)
	}

	srcEvents, err := store.Load("acc1")
	if err != nil {
		t.Fatalf("load acc1: %v", err)
	}
	src := domain.ReplayAccount("acc1", srcEvents)
	if src.Balance != 7500 {
		t.Errorf("replayed acc1 balance = %v, want 75", src.Balance)
	}

	dstEvents, err := store.Load("acc2")
	if err != nil {
		t.Fatalf("load acc2: %v", err)
	}
	dst := domain.ReplayAccount("acc2", dstEvents)
	if dst.Balance != 7500 {
		t.Errorf("replayed acc2 balance = %v, want 75", dst.Balance)
	}

	if captured.got.Amount != 2500 || captured.got.SourceAccountID != "acc1" || captured.got.DestinationAccountID != "acc2" {
		t.Errorf("captured event mismatch: %+v", captured.got)
	}
}

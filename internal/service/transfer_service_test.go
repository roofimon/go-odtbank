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
type complianceResult struct {
	approved bool
	err      error
}

func (c complianceResult) CheckTransfer(domain.TransferRecord) (bool, error) {
	return c.approved, c.err
}

func completeSaga(t *testing.T, svc *service.TransferService, id string) *domain.TransferRecord {
	t.Helper()
	for range 5 {
		r, err := svc.Find(id, "")
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != domain.TransferPending {
			return r
		}
		if err = svc.Process(r); err != nil {
			t.Fatal(err)
		}
	}
	r, err := svc.Find(id, "")
	if err != nil {
		t.Fatal(err)
	}
	return r
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
	completed := completeSaga(t, svc, first.TransferID)
	if completed.Status != domain.TransferCompleted {
		t.Fatalf("status=%s", completed.Status)
	}
	if published != 1 {
		t.Fatalf("published=%d", published)
	}
	cmd.Amount = 2000
	if _, err = svc.Transfer(cmd); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("conflict=%v", err)
	}
	events, _ := store.Load("a")
	if len(events) != 4 {
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
	completeSaga(t, svc, first)
	events, _ := store.Load("a")
	if len(events) != 4 {
		t.Fatalf("source events=%d", len(events))
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

	record := completeSaga(t, svc, receipt.TransferID)
	if got, want := record.InitialSourceBalance, domain.Money(10000); got != want {
		t.Errorf("initial source balance = %v, want %v", got, want)
	}
	if got, want := record.FinalSourceBalance, domain.Money(7500); got != want {
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

func TestTransferSaga_ComplianceRejectionReleasesReservation(t *testing.T) {
	store := eventstore.NewMemoryStore()
	_ = store.Append(domain.AccountOpened{Aggregate: "a", Type: "AccountOpened", ID: "a", InitialBalance: 10000, Occurred: time.Now()}, 0)
	_ = store.Append(domain.AccountOpened{Aggregate: "b", Type: "AccountOpened", ID: "b", Occurred: time.Now()}, 0)
	svc := service.NewTransferService(store, &policy.ZeroFeePolicy{}, &policy.DefaultTimeService{ServiceAvailable: true}, nil)
	svc.SetComplianceChecker(complianceResult{approved: false})
	receipt, err := svc.Transfer(domain.TransferCommand{Amount: 5000, SourceAccountID: "a", DestinationAccountID: "b", IdempotencyKey: "reject"})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := svc.Find(receipt.TransferID, "")
	if err = svc.Process(r); err != nil {
		t.Fatal(err)
	}
	events, _ := store.Load("a")
	if got := domain.ReplayAccount("a", events).AvailableBalance; got != 5000 {
		t.Fatalf("available while reserved=%d", got)
	}
	r, _ = svc.Find(receipt.TransferID, "")
	if err = svc.Process(r); err != nil {
		t.Fatal(err)
	}
	r, _ = svc.Find(receipt.TransferID, "")
	if r.Status != domain.TransferFailed || r.FailureCode != "compliance_rejected" {
		t.Fatalf("transfer=%+v", r)
	}
	events, _ = store.Load("a")
	a := domain.ReplayAccount("a", events)
	if a.Balance != 10000 || a.ReservedBalance != 0 || a.AvailableBalance != 10000 {
		t.Fatalf("account=%+v", a)
	}
}

func TestTransferSaga_ConcurrentReservationsCannotOverspend(t *testing.T) {
	store := eventstore.NewMemoryStore()
	_ = store.Append(domain.AccountOpened{Aggregate: "a", Type: "AccountOpened", ID: "a", InitialBalance: 10000, Occurred: time.Now()}, 0)
	_ = store.Append(domain.AccountOpened{Aggregate: "b", Type: "AccountOpened", ID: "b", Occurred: time.Now()}, 0)
	svc := service.NewTransferService(store, &policy.ZeroFeePolicy{}, &policy.DefaultTimeService{ServiceAvailable: true}, nil)
	one, _ := svc.Transfer(domain.TransferCommand{Amount: 8000, SourceAccountID: "a", DestinationAccountID: "b", IdempotencyKey: "one"})
	two, _ := svc.Transfer(domain.TransferCommand{Amount: 8000, SourceAccountID: "a", DestinationAccountID: "b", IdempotencyKey: "two"})
	r1, _ := svc.Find(one.TransferID, "")
	r2, _ := svc.Find(two.TransferID, "")
	if err := svc.Process(r1); err != nil {
		t.Fatal(err)
	}
	if err := svc.Process(r2); err != nil {
		t.Fatal(err)
	}
	r2, _ = svc.Find(two.TransferID, "")
	if r2.Status != domain.TransferFailed || r2.FailureCode != "insufficient_funds" {
		t.Fatalf("second=%+v", r2)
	}
	events, _ := store.Load("a")
	a := domain.ReplayAccount("a", events)
	if a.ReservedBalance != 8000 || a.AvailableBalance != 2000 {
		t.Fatalf("account=%+v", a)
	}
}

func TestTransferSaga_SideEffectsAreIdempotent(t *testing.T) {
	store := eventstore.NewMemoryStore()
	_ = store.Append(domain.AccountOpened{Aggregate: "a", Type: "AccountOpened", ID: "a", InitialBalance: 10000, Occurred: time.Now()}, 0)
	_ = store.Append(domain.AccountOpened{Aggregate: "b", Type: "AccountOpened", ID: "b", Occurred: time.Now()}, 0)
	r := domain.TransferRecord{ID: "trf_test", SourceAccountID: "a", DestinationAccountID: "b", Amount: 2000, Fee: 100}
	if _, err := store.ReserveTransferFunds(r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveTransferFunds(r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PostTransferLedger(r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PostTransferLedger(r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureTransferReservation(r, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.CaptureTransferReservation(r, time.Now()); err != nil {
		t.Fatal(err)
	}
	aEvents, _ := store.Load("a")
	bEvents, _ := store.Load("b")
	if len(aEvents) != 5 || len(bEvents) != 2 {
		t.Fatalf("event counts=%d,%d", len(aEvents), len(bEvents))
	}
	a := domain.ReplayAccount("a", aEvents)
	if a.Balance != 7900 || a.ReservedBalance != 0 {
		t.Fatalf("account=%+v", a)
	}
}

func TestTransferSaga_ExhaustedComplianceRetriesReleaseFunds(t *testing.T) {
	store := eventstore.NewMemoryStore()
	_ = store.Append(domain.AccountOpened{Aggregate: "a", Type: "AccountOpened", ID: "a", InitialBalance: 10000, Occurred: time.Now()}, 0)
	_ = store.Append(domain.AccountOpened{Aggregate: "b", Type: "AccountOpened", ID: "b", Occurred: time.Now()}, 0)
	svc := service.NewTransferService(store, &policy.ZeroFeePolicy{}, &policy.DefaultTimeService{ServiceAvailable: true}, nil)
	svc.SetComplianceChecker(complianceResult{err: errors.New("compliance unavailable")})
	receipt, _ := svc.Transfer(domain.TransferCommand{Amount: 3000, SourceAccountID: "a", DestinationAccountID: "b", IdempotencyKey: "retry"})
	r, _ := svc.Find(receipt.TransferID, "")
	if err := svc.Process(r); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		r, _ = svc.Find(receipt.TransferID, "")
		if r.Status != domain.TransferPending {
			break
		}
		if err := svc.Process(r); err != nil {
			t.Fatal(err)
		}
	}
	r, _ = svc.Find(receipt.TransferID, "")
	if r.Status != domain.TransferFailed || r.FailureCode != "retry_exhausted" || r.AttemptCount != 5 {
		t.Fatalf("transfer=%+v", r)
	}
	events, _ := store.Load("a")
	a := domain.ReplayAccount("a", events)
	if a.ReservedBalance != 0 || a.AvailableBalance != 10000 {
		t.Fatalf("account=%+v", a)
	}
}

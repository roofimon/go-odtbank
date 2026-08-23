package service_test

import (
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

func TestTransfer_AppendsEventsAndReplaysState(t *testing.T) {
	store := eventstore.NewMemoryStore()

	seed := func(id string, balance float64) {
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
	seed("acc1", 100.0)
	seed("acc2", 50.0)

	var captured capturedEvent
	bus := func(ev domain.TransferCompletedEvent) {
		captured.got = ev
	}

	svc := service.NewTransferService(store, &policy.ZeroFeePolicy{}, &policy.DefaultTimeService{ServiceAvailable: true}, bus)

	receipt, err := svc.Transfer(25.0, "acc1", "acc2")
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}

	if got, want := receipt.InitialSourceAccount.Balance, 100.0; got != want {
		t.Errorf("initial source balance = %v, want %v", got, want)
	}
	if got, want := receipt.InitialDestinationAccount.Balance, 50.0; got != want {
		t.Errorf("initial destination balance = %v, want %v", got, want)
	}
	if got, want := receipt.FinalSourceAccount.Balance, 75.0; got != want {
		t.Errorf("final source balance = %v, want %v", got, want)
	}
	if got, want := receipt.FinalDestinationAccount.Balance, 75.0; got != want {
		t.Errorf("final destination balance = %v, want %v", got, want)
	}

	srcEvents, err := store.Load("acc1")
	if err != nil {
		t.Fatalf("load acc1: %v", err)
	}
	src := domain.ReplayAccount("acc1", srcEvents)
	if src.Balance != 75.0 {
		t.Errorf("replayed acc1 balance = %v, want 75", src.Balance)
	}

	dstEvents, err := store.Load("acc2")
	if err != nil {
		t.Fatalf("load acc2: %v", err)
	}
	dst := domain.ReplayAccount("acc2", dstEvents)
	if dst.Balance != 75.0 {
		t.Errorf("replayed acc2 balance = %v, want 75", dst.Balance)
	}

	if captured.got.Amount != 25.0 || captured.got.SourceAccountID != "acc1" || captured.got.DestinationAccountID != "acc2" {
		t.Errorf("captured event mismatch: %+v", captured.got)
	}
}

package service_test

import (
	"errors"
	"testing"
	"time"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/policy"
	"go-odtbank/internal/service"
)

func seedAdjustmentAccount(t *testing.T, store *eventstore.MemoryStore, id string, balance domain.Money) {
	t.Helper()
	if err := store.Append(domain.AccountOpened{Aggregate: id, Type: "AccountOpened", ID: id, InitialBalance: balance, Occurred: time.Now()}, 0); err != nil {
		t.Fatal(err)
	}
}

func TestAdjustment_ManualCreditRequiresDifferentApprover(t *testing.T) {
	store := eventstore.NewMemoryStore()
	seedAdjustmentAccount(t, store, "account", 1000)
	svc := service.NewAdjustmentService(store)

	request, err := svc.Create(domain.AdjustmentRequest{Type: domain.AdjustmentManual, AccountID: "account", Direction: "credit", Amount: 250, Reason: "Correct duplicate fee", CaseReference: "CASE-1"}, "maker")
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Approve(request.ID, "maker"); !errors.Is(err, domain.ErrSelfApproval) {
		t.Fatalf("self approval error = %v", err)
	}
	if err = svc.Approve(request.ID, "checker"); err != nil {
		t.Fatal(err)
	}
	events, _ := store.Load("account")
	if got := domain.ReplayAccount("account", events).Balance; got != 1250 {
		t.Fatalf("balance = %d, want 1250", got)
	}
	credit, ok := events[len(events)-1].(domain.MoneyCredited)
	if !ok || credit.AdjustmentID != request.ID || credit.AdjustmentReason != "Correct duplicate fee" || credit.CaseReference != "CASE-1" {
		t.Fatalf("adjustment audit event = %#v", events[len(events)-1])
	}
}

func TestAdjustment_DebitCannotOverdrawAccount(t *testing.T) {
	store := eventstore.NewMemoryStore()
	seedAdjustmentAccount(t, store, "account", 1000)
	svc := service.NewAdjustmentService(store)
	request, err := svc.Create(domain.AdjustmentRequest{Type: domain.AdjustmentManual, AccountID: "account", Direction: "debit", Amount: 1001, Reason: "Correct excess credit", CaseReference: "CASE-2"}, "maker")
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Approve(request.ID, "checker"); err == nil {
		t.Fatal("expected insufficient funds")
	}
	events, _ := store.Load("account")
	if len(events) != 1 {
		t.Fatalf("failed adjustment appended %d events", len(events)-1)
	}
}

func TestAdjustment_TransferReversalRefundsFeeAndCanRunOnce(t *testing.T) {
	store := eventstore.NewMemoryStore()
	seedAdjustmentAccount(t, store, "source", 10000)
	seedAdjustmentAccount(t, store, "destination", 1000)
	transferService := service.NewTransferService(store, &policy.FlatFeePolicy{Fee: 100}, &policy.DefaultTimeService{ServiceAvailable: true}, nil)
	receipt, err := transferService.Transfer(domain.TransferCommand{SourceAccountID: "source", DestinationAccountID: "destination", Amount: 2000, IdempotencyKey: "reversal-test"})
	if err != nil {
		t.Fatal(err)
	}
	completeSaga(t, transferService, receipt.TransferID)

	svc := service.NewAdjustmentService(store)
	request, err := svc.Create(domain.AdjustmentRequest{Type: domain.AdjustmentReversal, OriginalTransferID: receipt.TransferID, Reason: "Reverse erroneous transfer", CaseReference: "CASE-3"}, "maker")
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Approve(request.ID, "checker"); err != nil {
		t.Fatal(err)
	}
	sourceEvents, _ := store.Load("source")
	destinationEvents, _ := store.Load("destination")
	if got := domain.ReplayAccount("source", sourceEvents).Balance; got != 10000 {
		t.Fatalf("source balance = %d, want 10000", got)
	}
	if got := domain.ReplayAccount("destination", destinationEvents).Balance; got != 1000 {
		t.Fatalf("destination balance = %d, want 1000", got)
	}
	_, err = svc.Create(domain.AdjustmentRequest{Type: domain.AdjustmentReversal, OriginalTransferID: receipt.TransferID, Reason: "Attempt second reversal", CaseReference: "CASE-4"}, "maker")
	if !errors.Is(err, domain.ErrAlreadyReversed) {
		t.Fatalf("second reversal error = %v", err)
	}
}

func TestAdjustment_RejectionDoesNotChangeBalance(t *testing.T) {
	store := eventstore.NewMemoryStore()
	seedAdjustmentAccount(t, store, "account", 1000)
	svc := service.NewAdjustmentService(store)
	request, err := svc.Create(domain.AdjustmentRequest{Type: domain.AdjustmentManual, AccountID: "account", Direction: "credit", Amount: 500, Reason: "Proposed correction", CaseReference: "CASE-5"}, "maker")
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Reject(request.ID, "checker", "Evidence does not support correction"); err != nil {
		t.Fatal(err)
	}
	events, _ := store.Load("account")
	if got := domain.ReplayAccount("account", events).Balance; got != 1000 {
		t.Fatalf("balance = %d, want 1000", got)
	}
}

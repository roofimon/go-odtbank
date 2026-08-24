package service_test

import (
	"errors"
	"math"
	"testing"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/service"
)

func TestWithdraw_AppendsDebitAndReturnsReceipt(t *testing.T) {
	for _, amount := range []float64{10, 25.50, 100} {
		t.Run("amount", func(t *testing.T) {
			store := seededDepositStore(t)
			svc := service.NewWithdrawService(store)

			receipt, err := svc.Withdraw(amount, "acc1")
			if err != nil {
				t.Fatalf("Withdraw: %v", err)
			}
			if receipt.InitialAccount.Balance != 100 || receipt.FinalAccount.Balance != 100-amount {
				t.Fatalf("unexpected receipt: %+v", receipt)
			}
			if receipt.WithdrawalAmount != amount {
				t.Fatalf("withdrawal amount = %v, want %v", receipt.WithdrawalAmount, amount)
			}

			events, err := store.Load("acc1")
			if err != nil {
				t.Fatalf("load events: %v", err)
			}
			if len(events) != 2 || events[1].Version() != 1 {
				t.Fatalf("events = %+v, want debit at sequence 1", events)
			}
			debit, ok := events[1].(domain.MoneyDebited)
			if !ok || debit.Amount != amount {
				t.Fatalf("event = %#v, want MoneyDebited(%v)", events[1], amount)
			}
		})
	}
}

func TestWithdraw_RejectsInvalidAmountsWithoutAppending(t *testing.T) {
	for _, amount := range []float64{-1, 0, 9.99, math.NaN(), math.Inf(1), math.Inf(-1)} {
		store := seededDepositStore(t)
		svc := service.NewWithdrawService(store)
		if _, err := svc.Withdraw(amount, "acc1"); !errors.Is(err, domain.ErrInvalidWithdrawAmount) {
			t.Errorf("Withdraw(%v) error = %v, want ErrInvalidWithdrawAmount", amount, err)
		}
		events, _ := store.Load("acc1")
		if len(events) != 1 {
			t.Errorf("Withdraw(%v) appended an event", amount)
		}
	}
}

func TestWithdraw_RejectsInsufficientFundsWithoutAppending(t *testing.T) {
	store := seededDepositStore(t)
	svc := service.NewWithdrawService(store)
	_, err := svc.Withdraw(110, "acc1")
	var fundsErr *domain.InsufficientFundsError
	if !errors.As(err, &fundsErr) {
		t.Fatalf("error = %v, want InsufficientFundsError", err)
	}
	events, _ := store.Load("acc1")
	if len(events) != 1 {
		t.Fatal("insufficient withdrawal appended an event")
	}
}

func TestWithdraw_AccountNotFound(t *testing.T) {
	svc := service.NewWithdrawService(eventstore.NewMemoryStore())
	if _, err := svc.Withdraw(10, "missing"); !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("error = %v, want ErrAccountNotFound", err)
	}
}

func TestWithdraw_PropagatesConcurrencyConflict(t *testing.T) {
	store := seededDepositStore(t)
	svc := service.NewWithdrawService(conflictingStore{store})
	if _, err := svc.Withdraw(10, "acc1"); !errors.Is(err, eventstore.ErrConcurrencyConflict) {
		t.Fatalf("error = %v, want ErrConcurrencyConflict", err)
	}
}

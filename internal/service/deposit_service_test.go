package service_test

import (
	"errors"
	"testing"
	"time"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/service"
)

func seededDepositStore(t *testing.T) *eventstore.MemoryStore {
	t.Helper()
	store := eventstore.NewMemoryStore()
	err := store.Append(domain.AccountOpened{
		Aggregate: "acc1", Type: "AccountOpened", Seq: 0,
		Occurred: time.Now(), ID: "acc1", InitialBalance: 10000,
	}, 0)
	if err != nil {
		t.Fatalf("seed account: %v", err)
	}
	return store
}

func TestDeposit_AppendsCreditAndReturnsReceipt(t *testing.T) {
	for _, amount := range []domain.Money{1000, 2550} {
		t.Run("amount", func(t *testing.T) {
			store := seededDepositStore(t)
			svc := service.NewDepositService(store)

			receipt, err := svc.Deposit(amount, "acc1")
			if err != nil {
				t.Fatalf("Deposit: %v", err)
			}
			if receipt.InitialAccount.Balance != 10000 || receipt.FinalAccount.Balance != 10000+amount {
				t.Fatalf("unexpected receipt: %+v", receipt)
			}

			events, err := store.Load("acc1")
			if err != nil {
				t.Fatalf("load events: %v", err)
			}
			if len(events) != 2 || events[1].Version() != 1 {
				t.Fatalf("events = %+v, want credit at sequence 1", events)
			}
			credit, ok := events[1].(domain.MoneyCredited)
			if !ok || credit.Amount != amount {
				t.Fatalf("event = %#v, want MoneyCredited(%v)", events[1], amount)
			}
		})
	}
}

func TestDeposit_RejectsInvalidAmountsWithoutAppending(t *testing.T) {
	for _, amount := range []domain.Money{-1, 0, 999} {
		store := seededDepositStore(t)
		svc := service.NewDepositService(store)
		if _, err := svc.Deposit(amount, "acc1"); !errors.Is(err, domain.ErrInvalidDepositAmount) {
			t.Errorf("Deposit(%v) error = %v, want ErrInvalidDepositAmount", amount, err)
		}
		events, _ := store.Load("acc1")
		if len(events) != 1 {
			t.Errorf("Deposit(%v) appended an event", amount)
		}
	}
}

func TestDeposit_AccountNotFound(t *testing.T) {
	svc := service.NewDepositService(eventstore.NewMemoryStore())
	if _, err := svc.Deposit(1000, "missing"); !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("error = %v, want ErrAccountNotFound", err)
	}
}

type conflictingStore struct{ *eventstore.MemoryStore }

func (conflictingStore) WithdrawAvailable(string, domain.Money, time.Time) (*domain.Account, *domain.Account, error) {
	return nil, nil, eventstore.ErrConcurrencyConflict
}

func (s conflictingStore) Append(domain.Event, int) error {
	return eventstore.ErrConcurrencyConflict
}

func TestDeposit_PropagatesConcurrencyConflict(t *testing.T) {
	store := seededDepositStore(t)
	svc := service.NewDepositService(conflictingStore{store})
	if _, err := svc.Deposit(1000, "acc1"); !errors.Is(err, eventstore.ErrConcurrencyConflict) {
		t.Fatalf("error = %v, want ErrConcurrencyConflict", err)
	}
}

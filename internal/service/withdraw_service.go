package service

import (
	"time"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
)

const minimumWithdrawAmount domain.Money = 1000

type WithdrawService struct {
	eventStore eventstore.Store
}
type atomicAvailableWithdrawer interface {
	WithdrawAvailable(accountID string, amount domain.Money, at time.Time) (*domain.Account, *domain.Account, error)
}

func NewWithdrawService(store eventstore.Store) *WithdrawService {
	return &WithdrawService{eventStore: store}
}

func (s *WithdrawService) Withdraw(amount domain.Money, accountID string) (*domain.WithdrawalReceipt, error) {
	if amount < minimumWithdrawAmount {
		return nil, domain.ErrInvalidWithdrawAmount
	}
	if atomic, ok := s.eventStore.(atomicAvailableWithdrawer); ok {
		initial, final, err := atomic.WithdrawAvailable(accountID, amount, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		return &domain.WithdrawalReceipt{InitialAccount: initial, FinalAccount: final, WithdrawalAmount: amount}, nil
	}

	events, err := s.eventStore.Load(accountID)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, domain.ErrAccountNotFound
	}

	initial := domain.ReplayAccount(accountID, events)
	if initial.AvailableBalance < amount {
		return nil, domain.NewInsufficientFundsError(initial, amount)
	}

	debit := domain.MoneyDebited{
		Aggregate: accountID,
		Type:      "MoneyDebited",
		Seq:       len(events),
		Occurred:  time.Now(),
		ID:        accountID,
		Amount:    amount,
	}
	if err := s.eventStore.Append(debit, debit.Seq); err != nil {
		return nil, err
	}

	return &domain.WithdrawalReceipt{
		InitialAccount:   initial,
		FinalAccount:     domain.ReplayAccount(accountID, append(events, debit)),
		WithdrawalAmount: amount,
	}, nil
}

var _ domain.WithdrawService = (*WithdrawService)(nil)

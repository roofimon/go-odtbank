package service

import (
	"math"
	"time"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
)

const minimumWithdrawAmount = 10.0

type WithdrawService struct {
	eventStore eventstore.Store
}

func NewWithdrawService(store eventstore.Store) *WithdrawService {
	return &WithdrawService{eventStore: store}
}

func (s *WithdrawService) Withdraw(amount float64, accountID string) (*domain.WithdrawalReceipt, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < minimumWithdrawAmount {
		return nil, domain.ErrInvalidWithdrawAmount
	}

	events, err := s.eventStore.Load(accountID)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, domain.ErrAccountNotFound
	}

	initial := domain.ReplayAccount(accountID, events)
	if initial.Balance < amount {
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

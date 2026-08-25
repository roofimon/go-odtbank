package service

import (
	"time"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
)

const minimumDepositAmount domain.Money = 1000

type DepositService struct {
	eventStore eventstore.Store
}

func NewDepositService(store eventstore.Store) *DepositService {
	return &DepositService{eventStore: store}
}

func (s *DepositService) Deposit(amount domain.Money, accountID string) (*domain.DepositReceipt, error) {
	if amount < minimumDepositAmount {
		return nil, domain.ErrInvalidDepositAmount
	}

	events, err := s.eventStore.Load(accountID)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, domain.ErrAccountNotFound
	}

	initial := domain.ReplayAccount(accountID, events)
	credit := domain.MoneyCredited{
		Aggregate: accountID,
		Type:      "MoneyCredited",
		Seq:       len(events),
		Occurred:  time.Now(),
		ID:        accountID,
		Amount:    amount,
	}
	if err := s.eventStore.Append(credit, credit.Seq); err != nil {
		return nil, err
	}

	return &domain.DepositReceipt{
		InitialAccount: initial,
		FinalAccount:   domain.ReplayAccount(accountID, append(events, credit)),
		DepositAmount:  amount,
	}, nil
}

var _ domain.DepositService = (*DepositService)(nil)

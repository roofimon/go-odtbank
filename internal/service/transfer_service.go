package service

import (
	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
	"time"
)

type TransferService struct {
	eventStore        eventstore.Store
	feePolicy         domain.FeePolicy
	timeService       domain.TimeService
	eventBus          func(event domain.TransferCompletedEvent)
	minTransferAmount float64
}

func NewTransferService(
	store eventstore.Store,
	fee domain.FeePolicy,
	ts domain.TimeService,
	eventBus func(event domain.TransferCompletedEvent),
) *TransferService {
	return &TransferService{
		eventStore:        store,
		feePolicy:         fee,
		timeService:       ts,
		eventBus:          eventBus,
		minTransferAmount: 1.0,
	}
}

func (s *TransferService) Transfer(amount float64, srcID, dstID string) (*domain.TransferReceipt, error) {
	if amount < s.minTransferAmount {
		return nil, domain.ErrInvalidTransferAmount
	}

	if !s.timeService.IsServiceAvailable(time.Now()) {
		return nil, domain.ErrOutOfService
	}

	srcEvents, err := s.eventStore.Load(srcID)
	if err != nil {
		return nil, err
	}
	if len(srcEvents) == 0 {
		return nil, domain.ErrAccountNotFound
	}

	dstEvents, err := s.eventStore.Load(dstID)
	if err != nil {
		return nil, err
	}
	if len(dstEvents) == 0 {
		return nil, domain.ErrAccountNotFound
	}

	srcAcct := domain.ReplayAccount(srcID, srcEvents)
	dstAcct := domain.ReplayAccount(dstID, dstEvents)

	initialSrc := *srcAcct
	initialDst := *dstAcct

	fee := s.feePolicy.CalculateFee(amount)
	totalDebit := amount
	if fee > 0 {
		totalDebit += fee
	}

	if srcAcct.Balance < totalDebit {
		return nil, domain.NewInsufficientFundsError(srcAcct, totalDebit)
	}

	now := time.Now()

	if fee > 0 {
		feeEvent := domain.MoneyDebited{
			Aggregate: srcID,
			Type:      "MoneyDebited",
			Seq:       len(srcEvents),
			Occurred:  now,
			ID:        srcID,
			Amount:    fee,
		}
		if err := s.eventStore.Append(feeEvent, feeEvent.Seq); err != nil {
			return nil, err
		}
		srcEvents = append(srcEvents, feeEvent)
	}

	debitEvent := domain.MoneyDebited{
		Aggregate: srcID,
		Type:      "MoneyDebited",
		Seq:       len(srcEvents),
		Occurred:  now,
		ID:        srcID,
		Amount:    amount,
	}
	if err := s.eventStore.Append(debitEvent, debitEvent.Seq); err != nil {
		return nil, err
	}

	creditEvent := domain.MoneyCredited{
		Aggregate: dstID,
		Type:      "MoneyCredited",
		Seq:       len(dstEvents),
		Occurred:  now,
		ID:        dstID,
		Amount:    amount,
	}
	if err := s.eventStore.Append(creditEvent, creditEvent.Seq); err != nil {
		return nil, err
	}

	finalSrc := domain.ReplayAccount(srcID, append(srcEvents, debitEvent))
	finalDst := domain.ReplayAccount(dstID, append(dstEvents, creditEvent))

	s.eventBus(domain.TransferCompletedEvent{
		Timestamp:            now,
		Amount:               amount,
		SourceAccountID:      srcID,
		DestinationAccountID: dstID,
		Fee:                  fee,
	})

	return &domain.TransferReceipt{
		InitialSourceAccount:      &initialSrc,
		InitialDestinationAccount: &initialDst,
		FinalSourceAccount:        finalSrc,
		FinalDestinationAccount:   finalDst,
		TransferAmount:            amount,
		FeeAmount:                 fee,
	}, nil
}

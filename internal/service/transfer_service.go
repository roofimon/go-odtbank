package service

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"go-odtbank/internal/domain"
)

type TransferService struct {
	store       domain.AtomicTransferStore
	feePolicy   domain.FeePolicy
	timeService domain.TimeService
	eventBus    func(domain.TransferCompletedEvent)
}

func NewTransferService(store domain.AtomicTransferStore, fee domain.FeePolicy, ts domain.TimeService, bus func(domain.TransferCompletedEvent)) *TransferService {
	return &TransferService{store: store, feePolicy: fee, timeService: ts, eventBus: bus}
}
func (s *TransferService) Transfer(command domain.TransferCommand) (*domain.TransferReceipt, error) {
	if command.Amount < 100 || command.SourceAccountID == command.DestinationAccountID {
		return nil, domain.ErrInvalidTransferAmount
	}
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	if command.IdempotencyKey == "" || len(command.IdempotencyKey) > 128 {
		return nil, domain.ErrIdempotencyKeyRequired
	}
	if !s.timeService.IsServiceAvailable(time.Now()) {
		return nil, domain.ErrOutOfService
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	fee := s.feePolicy.CalculateFee(command.Amount)
	const maxInt64 = int64(^uint64(0) >> 1)
	if fee < 0 || int64(command.Amount) > maxInt64-int64(fee) {
		return nil, domain.ErrInvalidTransferAmount
	}
	record, completedNow, err := s.store.ExecuteTransfer(domain.TransferRecord{ID: "trf_" + hex.EncodeToString(raw), SourceAccountID: command.SourceAccountID, DestinationAccountID: command.DestinationAccountID, IdempotencyKey: command.IdempotencyKey, Amount: command.Amount, Fee: fee, Status: domain.TransferPending, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return nil, err
	}
	if record.Status == domain.TransferFailed {
		return nil, transferFailure(record)
	}
	if completedNow && s.eventBus != nil {
		s.eventBus(domain.TransferCompletedEvent{TransferID: record.ID, Timestamp: record.UpdatedAt, Amount: record.Amount, SourceAccountID: record.SourceAccountID, DestinationAccountID: record.DestinationAccountID, Fee: record.Fee})
	}
	return &domain.TransferReceipt{InitialSourceAccount: &domain.Account{ID: record.SourceAccountID, Balance: record.InitialSourceBalance}, FinalSourceAccount: &domain.Account{ID: record.SourceAccountID, Balance: record.FinalSourceBalance}, TransferAmount: record.Amount, FeeAmount: record.Fee, TransferID: record.ID, Status: record.Status}, nil
}
func transferFailure(r *domain.TransferRecord) error {
	switch r.FailureCode {
	case "account_not_found":
		return domain.ErrAccountNotFound
	case "insufficient_funds":
		return domain.NewInsufficientFundsError(&domain.Account{ID: r.SourceAccountID, Balance: r.InitialSourceBalance}, r.Amount+r.Fee)
	default:
		return domain.ErrInvalidTransferAmount
	}
}
func (s *TransferService) Find(id, sourceID string) (*domain.TransferRecord, error) {
	r, err := s.store.FindTransfer(id)
	if err != nil {
		return nil, err
	}
	if sourceID != "" && r.SourceAccountID != sourceID {
		return nil, domain.ErrForbidden
	}
	return r, nil
}

var _ domain.TransferService = (*TransferService)(nil)

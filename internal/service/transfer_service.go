package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"go-odtbank/internal/domain"
)

type approveAllCompliance struct{}

func (approveAllCompliance) CheckTransfer(domain.TransferRecord) (bool, error) { return true, nil }

type TransferService struct {
	store       domain.TransferSagaStore
	feePolicy   domain.FeePolicy
	timeService domain.TimeService
	compliance  domain.ComplianceChecker
	wake        chan struct{}
}

func NewTransferService(store domain.TransferSagaStore, fee domain.FeePolicy, ts domain.TimeService) *TransferService {
	return &TransferService{store: store, feePolicy: fee, timeService: ts, compliance: approveAllCompliance{}, wake: make(chan struct{}, 1)}
}
func (s *TransferService) SetComplianceChecker(checker domain.ComplianceChecker) {
	if checker != nil {
		s.compliance = checker
	}
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
	record, _, err := s.store.CreateTransfer(domain.TransferRecord{ID: "trf_" + hex.EncodeToString(raw), SourceAccountID: command.SourceAccountID, DestinationAccountID: command.DestinationAccountID, IdempotencyKey: command.IdempotencyKey, Amount: command.Amount, Fee: fee, Status: domain.TransferPending, CurrentStep: domain.SagaCreated, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return nil, err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return receiptFromTransfer(record), nil
}
func receiptFromTransfer(r *domain.TransferRecord) *domain.TransferReceipt {
	return &domain.TransferReceipt{InitialSourceAccount: &domain.Account{ID: r.SourceAccountID, Balance: r.InitialSourceBalance}, FinalSourceAccount: &domain.Account{ID: r.SourceAccountID, Balance: r.FinalSourceBalance}, TransferAmount: r.Amount, FeeAmount: r.Fee, TransferID: r.ID, Status: r.Status, CurrentStep: r.CurrentStep}
}
func (s *TransferService) Find(id, sourceID string) (*domain.TransferRecord, error) {
	r, e := s.store.FindTransfer(id)
	if e != nil {
		return nil, e
	}
	if sourceID != "" && r.SourceAccountID != sourceID {
		return nil, domain.ErrForbidden
	}
	return r, nil
}

func (s *TransferService) Run(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
		_ = s.ProcessDue(ctx)
	}
}
func (s *TransferService) ProcessDue(ctx context.Context) error {
	items, e := s.store.ListDueTransfers(time.Now().UTC(), 100)
	if e != nil {
		return e
	}
	for i := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = s.Process(&items[i])
	}
	return nil
}

func (s *TransferService) Process(r *domain.TransferRecord) error {
	now := time.Now().UTC()
	switch r.CurrentStep {
	case domain.SagaCreated:
		balance, e := s.store.ReserveTransferFunds(*r, now)
		if e != nil {
			return s.handleStepError(r, e, false)
		}
		return s.advance(r, domain.TransferSagaUpdate{ExpectedStep: r.CurrentStep, CurrentStep: domain.SagaFundsReserved, Status: domain.TransferPending, InitialSourceBalance: balance, NextAttemptAt: now})
	case domain.SagaFundsReserved:
		approved, e := s.compliance.CheckTransfer(*r)
		if e != nil {
			return s.handleStepError(r, e, true)
		}
		decision := "rejected"
		if approved {
			decision = "approved"
		}
		if e = s.store.RecordComplianceDecision(r.ID, decision, now); e != nil {
			return s.handleStepError(r, e, true)
		}
		if !approved {
			if e = s.store.ReleaseTransferReservation(*r, now); e != nil {
				return s.handleStepError(r, e, true)
			}
			return s.advance(r, domain.TransferSagaUpdate{ExpectedStep: r.CurrentStep, CurrentStep: r.CurrentStep, Status: domain.TransferFailed, FailureCode: "compliance_rejected", ComplianceStatus: decision, NextAttemptAt: now})
		}
		return s.advance(r, domain.TransferSagaUpdate{ExpectedStep: r.CurrentStep, CurrentStep: domain.SagaComplianceApproved, Status: domain.TransferPending, ComplianceStatus: decision, NextAttemptAt: now})
	case domain.SagaComplianceApproved:
		final, e := s.store.PostTransferLedger(*r, now)
		if e != nil {
			return s.handleStepError(r, e, true)
		}
		return s.advance(r, domain.TransferSagaUpdate{ExpectedStep: r.CurrentStep, CurrentStep: domain.SagaLedgerPosted, Status: domain.TransferPending, FinalSourceBalance: final, NextAttemptAt: now})
	case domain.SagaLedgerPosted:
		if e := s.store.CaptureTransferReservation(*r, now); e != nil {
			return s.retry(r, e, true)
		}
		return s.advance(r, domain.TransferSagaUpdate{ExpectedStep: r.CurrentStep, CurrentStep: domain.SagaReservationCaptured, Status: domain.TransferPending, NextAttemptAt: now})
	case domain.SagaReservationCaptured:
		_, e := s.store.UpdateTransferSaga(r.ID, domain.TransferSagaUpdate{ExpectedStep: r.CurrentStep, CurrentStep: r.CurrentStep, Status: domain.TransferCompleted, NextAttemptAt: now}, now)
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *TransferService) handleStepError(r *domain.TransferRecord, stepErr error, reserved bool) error {
	var funds *domain.InsufficientFundsError
	permanent := errors.Is(stepErr, domain.ErrAccountNotFound) || errors.As(stepErr, &funds)
	if permanent {
		if reserved {
			_ = s.store.ReleaseTransferReservation(*r, time.Now().UTC())
		}
		code := "account_not_found"
		if funds != nil {
			code = "insufficient_funds"
		}
		return s.advance(r, domain.TransferSagaUpdate{ExpectedStep: r.CurrentStep, CurrentStep: r.CurrentStep, Status: domain.TransferFailed, FailureCode: code, LastError: stepErr.Error(), NextAttemptAt: time.Now().UTC()})
	}
	return s.retry(r, stepErr, false)
}
func (s *TransferService) retry(r *domain.TransferRecord, stepErr error, afterLedger bool) error {
	attempt := r.AttemptCount + 1
	if !afterLedger && attempt >= 5 {
		if r.CurrentStep != domain.SagaCreated {
			_ = s.store.ReleaseTransferReservation(*r, time.Now().UTC())
		}
		return s.advance(r, domain.TransferSagaUpdate{ExpectedStep: r.CurrentStep, CurrentStep: r.CurrentStep, Status: domain.TransferFailed, FailureCode: "retry_exhausted", LastError: stepErr.Error(), AttemptCount: attempt, NextAttemptAt: time.Now().UTC()})
	}
	delay := time.Second << min(attempt-1, 4)
	if afterLedger && delay > 30*time.Second {
		delay = 30 * time.Second
	}
	return s.advance(r, domain.TransferSagaUpdate{ExpectedStep: r.CurrentStep, CurrentStep: r.CurrentStep, Status: domain.TransferPending, LastError: stepErr.Error(), AttemptCount: attempt, NextAttemptAt: time.Now().UTC().Add(delay)})
}
func (s *TransferService) advance(r *domain.TransferRecord, u domain.TransferSagaUpdate) error {
	_, e := s.store.UpdateTransferSaga(r.ID, u, time.Now().UTC())
	return e
}

var _ domain.TransferService = (*TransferService)(nil)

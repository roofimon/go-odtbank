package domain

import (
	"errors"
	"time"
)

const (
	TransferPending         = "pending"
	TransferCompleted       = "completed"
	TransferFailed          = "failed"
	SagaCreated             = "created"
	SagaFundsReserved       = "funds_reserved"
	SagaComplianceApproved  = "compliance_approved"
	SagaLedgerPosted        = "ledger_posted"
	SagaReservationCaptured = "reservation_captured"
)

type TransferCommand struct {
	Amount                                                Money
	SourceAccountID, DestinationAccountID, IdempotencyKey string
}
type TransferRecord struct {
	ID                   string    `json:"transfer_id"`
	SourceAccountID      string    `json:"source_account_id"`
	DestinationAccountID string    `json:"destination_account_id"`
	IdempotencyKey       string    `json:"-"`
	Amount               Money     `json:"amount"`
	Fee                  Money     `json:"fee"`
	Status               string    `json:"status"`
	FailureCode          string    `json:"failure_code,omitempty"`
	InitialSourceBalance Money     `json:"initial_source_balance"`
	FinalSourceBalance   Money     `json:"final_source_balance"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	CurrentStep          string    `json:"current_step"`
	ComplianceStatus     string    `json:"compliance_status,omitempty"`
	AttemptCount         int       `json:"attempt_count,omitempty"`
	NextAttemptAt        time.Time `json:"next_attempt_at,omitempty"`
	LastError            string    `json:"last_error,omitempty"`
}

type TransferSagaUpdate struct {
	ExpectedStep, CurrentStep, Status, FailureCode, ComplianceStatus, LastError string
	AttemptCount                                                                int
	NextAttemptAt                                                               time.Time
	InitialSourceBalance, FinalSourceBalance                                    Money
}

type TransferSagaStore interface {
	CreateTransfer(record TransferRecord) (*TransferRecord, bool, error)
	FindTransfer(id string) (*TransferRecord, error)
	ListDueTransfers(now time.Time, limit int) ([]TransferRecord, error)
	UpdateTransferSaga(id string, update TransferSagaUpdate, at time.Time) (bool, error)
	ReserveTransferFunds(record TransferRecord, at time.Time) (Money, error)
	RecordComplianceDecision(transferID, decision string, at time.Time) error
	PostTransferLedger(record TransferRecord, at time.Time) (Money, error)
	CaptureTransferReservation(record TransferRecord, at time.Time) error
	ReleaseTransferReservation(record TransferRecord, at time.Time) error
}

type ComplianceChecker interface {
	CheckTransfer(record TransferRecord) (bool, error)
}

var (
	ErrIdempotencyKeyRequired = errors.New("Idempotency-Key header is required")
	ErrIdempotencyConflict    = errors.New("idempotency key was already used with different transfer details")
	ErrTransferNotFound       = errors.New("transfer not found")
)

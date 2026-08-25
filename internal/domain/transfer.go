package domain

import (
	"errors"
	"time"
)

const (
	TransferPending   = "pending"
	TransferCompleted = "completed"
	TransferFailed    = "failed"
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
}
type AtomicTransferStore interface {
	ExecuteTransfer(record TransferRecord) (*TransferRecord, bool, error)
	FindTransfer(id string) (*TransferRecord, error)
}

var (
	ErrIdempotencyKeyRequired = errors.New("Idempotency-Key header is required")
	ErrIdempotencyConflict    = errors.New("idempotency key was already used with different transfer details")
	ErrTransferNotFound       = errors.New("transfer not found")
)

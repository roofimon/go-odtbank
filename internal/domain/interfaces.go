package domain

import (
	"errors"
	"time"
)

type AccountRepository interface {
	FindByID(id string) (*Account, error)
	UpdateBalance(account *Account) error
}

type FeePolicy interface {
	CalculateFee(amount float64) float64
}

type TimeService interface {
	IsServiceAvailable(t time.Time) bool
}

type TransferService interface {
	Transfer(amount float64, srcAcctID, dstAcctID string) (*TransferReceipt, error)
}

var (
	ErrAccountNotFound       = errors.New("account not found")
	ErrInvalidTransferAmount = errors.New("invalid transfer amount")
	ErrOutOfService          = errors.New("service is out of service")
)

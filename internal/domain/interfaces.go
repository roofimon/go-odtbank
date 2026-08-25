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
	CalculateFee(amount Money) Money
}

type TimeService interface {
	IsServiceAvailable(t time.Time) bool
}

type TransferService interface {
	Transfer(command TransferCommand) (*TransferReceipt, error)
	Find(id, sourceAccountID string) (*TransferRecord, error)
}

type DepositService interface {
	Deposit(amount Money, accountID string) (*DepositReceipt, error)
}

type WithdrawService interface {
	Withdraw(amount Money, accountID string) (*WithdrawalReceipt, error)
}

var (
	ErrAccountNotFound       = errors.New("account not found")
	ErrInvalidTransferAmount = errors.New("invalid transfer amount")
	ErrInvalidDepositAmount  = errors.New("invalid deposit amount")
	ErrInvalidWithdrawAmount = errors.New("invalid withdrawal amount")
	ErrOutOfService          = errors.New("service is out of service")
)

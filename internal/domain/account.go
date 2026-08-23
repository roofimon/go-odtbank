package domain

import "time"

// Account is a stateless view over its event stream.
// Balance is derived by folding events; it is never mutated directly.
type Account struct {
	ID      string
	Balance float64
}

// ReplayAccount reconstructs an Account by folding its event history.
// The returned Account reflects the cumulative effect of all events seen.
func ReplayAccount(id string, events []Event) *Account {
	a := &Account{ID: id}
	for _, ev := range events {
		switch e := ev.(type) {
		case AccountOpened:
			a.Balance = e.InitialBalance
		case MoneyCredited:
			a.Balance += e.Amount
		case MoneyDebited:
			a.Balance -= e.Amount
		}
	}
	return a
}

type TransferReceipt struct {
	InitialSourceAccount      *Account
	InitialDestinationAccount *Account
	FinalSourceAccount        *Account
	FinalDestinationAccount   *Account
	TransferAmount            float64
	FeeAmount                 float64
}

// TransferCompletedEvent is an integration-level event published after a
// successful transfer. It is distinct from per-account stored events; downstream
// listeners (audit, analytics) can subscribe to it without touching the event store.
type TransferCompletedEvent struct {
	Timestamp            time.Time
	Amount               float64
	SourceAccountID      string
	DestinationAccountID string
	Fee                  float64
}

type InsufficientFundsError struct {
	Account *Account
	Amount  float64
}

func (e *InsufficientFundsError) Error() string {
	return "insufficient funds in account"
}

func NewInsufficientFundsError(a *Account, amount float64) *InsufficientFundsError {
	return &InsufficientFundsError{Account: a, Amount: amount}
}
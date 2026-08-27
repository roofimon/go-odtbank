package domain

import "time"

// Account is a stateless view over its event stream.
// Balance is derived by folding events; it is never mutated directly.
type Account struct {
	ID               string
	Balance          Money
	ReservedBalance  Money
	AvailableBalance Money
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
		case FundsReserved:
			a.ReservedBalance += e.Amount
		case ReservationCaptured:
			a.ReservedBalance -= e.Amount
		case ReservationReleased:
			a.ReservedBalance -= e.Amount
		}
	}
	a.AvailableBalance = a.Balance - a.ReservedBalance
	return a
}

type TransferReceipt struct {
	TransferID                string
	Status                    string
	InitialSourceAccount      *Account
	InitialDestinationAccount *Account
	FinalSourceAccount        *Account
	FinalDestinationAccount   *Account
	TransferAmount            Money
	FeeAmount                 Money
	CurrentStep               string
}

type DepositReceipt struct {
	InitialAccount *Account
	FinalAccount   *Account
	DepositAmount  Money
}

type WithdrawalReceipt struct {
	InitialAccount   *Account
	FinalAccount     *Account
	WithdrawalAmount Money
}

// TransferCompletedEvent is an integration-level event published after a
// successful transfer. It is distinct from per-account stored events; downstream
// listeners (audit, analytics) can subscribe to it without touching the event store.
type TransferCompletedEvent struct {
	Timestamp            time.Time
	TransferID           string
	Amount               Money
	SourceAccountID      string
	DestinationAccountID string
	Fee                  Money
}

type InsufficientFundsError struct {
	Account *Account
	Amount  Money
}

func (e *InsufficientFundsError) Error() string {
	return "insufficient funds in account"
}

func NewInsufficientFundsError(a *Account, amount Money) *InsufficientFundsError {
	return &InsufficientFundsError{Account: a, Amount: amount}
}

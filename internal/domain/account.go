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

// AccountSnapshot is a materialized view of an account's state at a point in
// time. AsOfSequence is the stream sequence the snapshot already includes, so the
// current state is snapshot + events with Version() > AsOfSequence.
type AccountSnapshot struct {
	AggregateID      string
	Balance          Money
	ReservedBalance  Money
	AvailableBalance Money
	AsOfSequence     int
	OccurredAt       time.Time
}

// ReplayAccount reconstructs an Account by folding its entire event history.
func ReplayAccount(id string, events []Event) *Account {
	return ReplayAccountFrom(id, nil, events)
}

// ReplayAccountFrom reconstructs an Account starting from an optional snapshot.
// When snap is non-nil, only events with Version() > snap.AsOfSequence are
// folded, so a long stream is read as the snapshot plus its tail. When snap is
// nil, this behaves like ReplayAccount.
func ReplayAccountFrom(id string, snap *AccountSnapshot, events []Event) *Account {
	if snap != nil && snap.AggregateID == id {
		a := &Account{ID: id, Balance: snap.Balance, ReservedBalance: snap.ReservedBalance}
		cutoff := snap.AsOfSequence
		for _, ev := range events {
			if ev.Version() <= cutoff {
				continue
			}
			fold(a, ev)
		}
		a.AvailableBalance = a.Balance - a.ReservedBalance
		return a
	}

	a := &Account{ID: id}
	for _, ev := range events {
		fold(a, ev)
	}
	a.AvailableBalance = a.Balance - a.ReservedBalance
	return a
}

func fold(a *Account, ev Event) {
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

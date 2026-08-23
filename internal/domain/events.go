package domain

import "time"

// Event is the base interface for all stored domain events.
// Events are immutable facts. They describe what happened, not what to do.
type Event interface {
	AggregateID() string
	EventType() string
	Version() int
	OccurredAt() time.Time
}

// AccountOpened records that a new account came into existence with an initial balance.
type AccountOpened struct {
	Aggregate      string
	Type           string
	Seq            int
	Occurred       time.Time
	ID             string
	InitialBalance float64
}

func (e AccountOpened) AggregateID() string   { return e.Aggregate }
func (e AccountOpened) EventType() string     { return e.Type }
func (e AccountOpened) Version() int          { return e.Seq }
func (e AccountOpened) OccurredAt() time.Time { return e.Occurred }

// MoneyDebited records that funds were removed from an account.
type MoneyDebited struct {
	Aggregate string
	Type      string
	Seq       int
	Occurred  time.Time
	ID        string
	Amount    float64
}

func (e MoneyDebited) AggregateID() string   { return e.Aggregate }
func (e MoneyDebited) EventType() string     { return e.Type }
func (e MoneyDebited) Version() int          { return e.Seq }
func (e MoneyDebited) OccurredAt() time.Time { return e.Occurred }

// MoneyCredited records that funds were added to an account.
type MoneyCredited struct {
	Aggregate string
	Type      string
	Seq       int
	Occurred  time.Time
	ID        string
	Amount    float64
}

func (e MoneyCredited) AggregateID() string   { return e.Aggregate }
func (e MoneyCredited) EventType() string     { return e.Type }
func (e MoneyCredited) Version() int          { return e.Seq }
func (e MoneyCredited) OccurredAt() time.Time { return e.Occurred }

package domain

import (
	"testing"
	"time"
)

func buildMoneyStream(id string, open, credits, debits, reserved int) []Event {
	oc := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	events := []Event{AccountOpened{Aggregate: id, Type: "AccountOpened", Seq: 0, Occurred: oc, ID: id, InitialBalance: Money(open)}}
	seq := 1
	for i := 0; i < credits; i++ {
		events = append(events, MoneyCredited{Aggregate: id, Type: "MoneyCredited", Seq: seq, Occurred: oc.Add(time.Duration(i) * time.Second), ID: id, Amount: Money(100)})
		seq++
	}
	for i := 0; i < debits; i++ {
		events = append(events, MoneyDebited{Aggregate: id, Type: "MoneyDebited", Seq: seq, Occurred: oc.Add(time.Duration(i) * time.Second), ID: id, Amount: Money(50)})
		seq++
	}
	for i := 0; i < reserved; i++ {
		amount := Money(200)
		events = append(events, FundsReserved{Aggregate: id, Type: "FundsReserved", Seq: seq, Occurred: oc.Add(time.Duration(i+100) * time.Second), ID: id, Amount: amount, TransferID: "t" + string(rune('A'+i))})
		seq++
		events = append(events, ReservationCaptured{Aggregate: id, Type: "ReservationCaptured", Seq: seq, Occurred: oc.Add(time.Duration(i+101) * time.Second), ID: id, Amount: amount, TransferID: "t" + string(rune('A'+i))})
		seq++
	}
	return events
}

func snapshotAt(events []Event, cutoff int) *AccountSnapshot {
	folded := ReplayAccountFrom(events[0].AggregateID(), nil, events[:cutoff+1])
	return &AccountSnapshot{
		AggregateID:       events[0].AggregateID(),
		Balance:          folded.Balance,
		ReservedBalance:  folded.ReservedBalance,
		AvailableBalance: folded.AvailableBalance,
		AsOfSequence:     events[cutoff].Version(),
		OccurredAt:       events[cutoff].OccurredAt(),
	}
}

func TestReplayAccountFromMatchesFullReplay(t *testing.T) {
	events := buildMoneyStream("acc1", 1000, 7, 3, 2)

	want := ReplayAccount("acc1", events)
	for cutoff := -1; cutoff < len(events); cutoff++ {
		var snap *AccountSnapshot
		if cutoff >= 0 {
			// A snapshot at a boundary records the state of folding every event up
			// to that sequence; folding the tail must recover the full replay.
			snap = snapshotAt(events, cutoff)
			}
		got := ReplayAccountFrom("acc1", snap, events)
		if got.Balance != want.Balance || got.ReservedBalance != want.ReservedBalance || got.AvailableBalance != want.AvailableBalance {
			t.Fatalf("cutoff=%d got=%+v want=%+v", cutoff, got, want)
			}
	}
}

func TestReplayAccountNilSnapshotEqualsReplayAccount(t *testing.T) {
	events := buildMoneyStream("acc1", 1000, 7, 3, 2)
	got := ReplayAccountFrom("acc1", nil, events)
	want := ReplayAccount("acc1", events)
	if got.Balance != want.Balance || got.AvailableBalance != want.AvailableBalance {
		t.Fatalf("got=%+v want=%+v", got, want)
	}
}

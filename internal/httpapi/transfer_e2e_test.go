package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/httpapi"
	"go-odtbank/internal/policy"
	"go-odtbank/internal/service"
)

func TestTransferEndToEnd(t *testing.T) {
	store := eventstore.NewMemoryStore()
	seedE2EAccount(t, store, "acc1", 100)
	seedE2EAccount(t, store, "acc2", 50)

	var completedEvent domain.TransferCompletedEvent
	transferService := service.NewTransferService(
		store,
		&policy.ZeroFeePolicy{},
		&policy.DefaultTimeService{ServiceAvailable: true},
		func(event domain.TransferCompletedEvent) { completedEvent = event },
	)
	server := httptest.NewServer(httpapi.NewRouter(httpapi.Dependencies{
		Store:           store,
		TransferService: transferService,
	}))
	t.Cleanup(server.Close)

	requestBody, err := json.Marshal(map[string]any{
		"amount":                 25.0,
		"source_account_id":      "acc1",
		"destination_account_id": "acc2",
	})
	if err != nil {
		t.Fatalf("encode transfer request: %v", err)
	}
	response, err := server.Client().Post(
		server.URL+"/transfer",
		"application/json",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("POST /transfer: %v", err)
	}

	var receipt struct {
		InitialSourceAccount *domain.Account
		FinalSourceAccount   *domain.Account
		DestinationAccountID string
		TransferAmount       float64
		FeeAmount            float64
	}
	decodeResponse(t, response, http.StatusOK, &receipt)
	if receipt.InitialSourceAccount.Balance != 100 || receipt.FinalSourceAccount.Balance != 75 {
		t.Errorf("source balances = %v -> %v, want 100 -> 75", receipt.InitialSourceAccount.Balance, receipt.FinalSourceAccount.Balance)
	}
	if receipt.DestinationAccountID != "acc2" {
		t.Errorf("destination account = %q, want acc2", receipt.DestinationAccountID)
	}
	if completedEvent.Amount != 25 || completedEvent.SourceAccountID != "acc1" || completedEvent.DestinationAccountID != "acc2" {
		t.Errorf("completed event = %+v", completedEvent)
	}

	assertE2EAccount(t, server, "acc1", 75, 2)
	assertE2EAccount(t, server, "acc2", 75, 2)
	assertE2ELastEvent(t, server, "acc1", 1, "MoneyDebited", 25)
	assertE2ELastEvent(t, server, "acc2", 1, "MoneyCredited", 25)
}

func seedE2EAccount(t *testing.T, store *eventstore.MemoryStore, id string, balance float64) {
	t.Helper()
	err := store.Append(domain.AccountOpened{
		Aggregate:      id,
		Type:           "AccountOpened",
		Seq:            0,
		Occurred:       time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC),
		ID:             id,
		InitialBalance: balance,
	}, 0)
	if err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func assertE2EAccount(t *testing.T, server *httptest.Server, id string, wantBalance float64, wantEventCount int) {
	t.Helper()
	response, err := server.Client().Get(server.URL + "/accounts")
	if err != nil {
		t.Fatalf("GET /accounts: %v", err)
	}
	var result struct {
		Accounts []struct {
			ID         string  `json:"id"`
			Balance    float64 `json:"balance"`
			EventCount int     `json:"event_count"`
		} `json:"accounts"`
	}
	decodeResponse(t, response, http.StatusOK, &result)
	for _, account := range result.Accounts {
		if account.ID == id {
			if account.Balance != wantBalance || account.EventCount != wantEventCount {
				t.Errorf("account %s = %+v, want balance %v with %d events", id, account, wantBalance, wantEventCount)
			}
			return
		}
	}
	t.Errorf("account %s not found in response", id)
}

func assertE2ELastEvent(t *testing.T, server *httptest.Server, id string, wantSeq int, wantType string, wantAmount float64) {
	t.Helper()
	response, err := server.Client().Get(server.URL + "/accounts/" + id + "/events")
	if err != nil {
		t.Fatalf("GET /accounts/%s/events: %v", id, err)
	}
	var result struct {
		AggregateID string `json:"aggregate_id"`
		Events      []struct {
			Seq    int     `json:"seq"`
			Type   string  `json:"type"`
			Amount float64 `json:"amount"`
		} `json:"events"`
	}
	decodeResponse(t, response, http.StatusOK, &result)
	if result.AggregateID != id || len(result.Events) == 0 {
		t.Fatalf("event log = %+v, want events for %s", result, id)
	}
	event := result.Events[len(result.Events)-1]
	if event.Seq != wantSeq || event.Type != wantType || event.Amount != wantAmount {
		t.Errorf("last event = %+v, want sequence %d %s for %v", event, wantSeq, wantType, wantAmount)
	}
}

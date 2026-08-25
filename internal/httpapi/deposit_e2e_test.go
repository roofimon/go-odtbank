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
	"go-odtbank/internal/service"
)

func TestDepositEndToEnd(t *testing.T) {
	store := eventstore.NewMemoryStore()
	err := store.Append(domain.AccountOpened{
		Aggregate:      "acc1",
		Type:           "AccountOpened",
		Seq:            0,
		Occurred:       time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC),
		ID:             "acc1",
		InitialBalance: 10000,
	}, 0)
	if err != nil {
		t.Fatalf("seed account: %v", err)
	}

	server := httptest.NewServer(httpapi.NewRouter(httpapi.Dependencies{
		Store:          store,
		DepositService: service.NewDepositService(store),
	}))
	t.Cleanup(server.Close)

	depositBody, err := json.Marshal(map[string]any{
		"account_id": "acc1",
		"amount":     25.0,
	})
	if err != nil {
		t.Fatalf("encode deposit request: %v", err)
	}
	response, err := server.Client().Post(
		server.URL+"/deposit",
		"application/json",
		bytes.NewReader(depositBody),
	)
	if err != nil {
		t.Fatalf("POST /deposit: %v", err)
	}

	var receipt domain.DepositReceipt
	decodeResponse(t, response, http.StatusOK, &receipt)
	if receipt.InitialAccount.ID != "acc1" || receipt.InitialAccount.Balance != 10000 {
		t.Errorf("initial account = %+v, want acc1 with balance 100", receipt.InitialAccount)
	}
	if receipt.FinalAccount.ID != "acc1" || receipt.FinalAccount.Balance != 12500 {
		t.Errorf("final account = %+v, want acc1 with balance 125", receipt.FinalAccount)
	}
	if receipt.DepositAmount != 2500 {
		t.Errorf("deposit amount = %v, want 25", receipt.DepositAmount)
	}

	accountsResponse, err := server.Client().Get(server.URL + "/accounts")
	if err != nil {
		t.Fatalf("GET /accounts: %v", err)
	}
	var accounts struct {
		Accounts []struct {
			ID         string  `json:"id"`
			Balance    float64 `json:"balance"`
			EventCount int     `json:"event_count"`
		} `json:"accounts"`
	}
	decodeResponse(t, accountsResponse, http.StatusOK, &accounts)
	if len(accounts.Accounts) != 1 {
		t.Fatalf("account count = %d, want 1", len(accounts.Accounts))
	}
	account := accounts.Accounts[0]
	if account.ID != "acc1" || account.Balance != 125 || account.EventCount != 2 {
		t.Errorf("account projection = %+v, want acc1 balance 125 with 2 events", account)
	}

	eventsResponse, err := server.Client().Get(server.URL + "/accounts/acc1/events")
	if err != nil {
		t.Fatalf("GET /accounts/acc1/events: %v", err)
	}
	var eventLog struct {
		AggregateID string `json:"aggregate_id"`
		Events      []struct {
			Seq    int     `json:"seq"`
			Type   string  `json:"type"`
			Amount float64 `json:"amount"`
		} `json:"events"`
	}
	decodeResponse(t, eventsResponse, http.StatusOK, &eventLog)
	if eventLog.AggregateID != "acc1" || len(eventLog.Events) != 2 {
		t.Fatalf("event log = %+v, want acc1 with 2 events", eventLog)
	}
	credit := eventLog.Events[1]
	if credit.Seq != 1 || credit.Type != "MoneyCredited" || credit.Amount != 25 {
		t.Errorf("deposit event = %+v, want sequence 1 MoneyCredited for 25", credit)
	}
}

func decodeResponse(t *testing.T, response *http.Response, wantStatus int, target any) {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		t.Fatalf("status = %d, want %d", response.StatusCode, wantStatus)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

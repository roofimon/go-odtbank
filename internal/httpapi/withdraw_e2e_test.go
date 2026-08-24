package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/httpapi"
	"go-odtbank/internal/service"
)

func TestWithdrawEndToEnd(t *testing.T) {
	store := eventstore.NewMemoryStore()
	seedE2EAccount(t, store, "acc1", 100)

	server := httptest.NewServer(httpapi.NewRouter(httpapi.Dependencies{
		Store:           store,
		WithdrawService: service.NewWithdrawService(store),
	}))
	t.Cleanup(server.Close)

	requestBody, err := json.Marshal(map[string]any{
		"account_id": "acc1",
		"amount":     40.0,
	})
	if err != nil {
		t.Fatalf("encode withdrawal request: %v", err)
	}
	response, err := server.Client().Post(
		server.URL+"/withdraw",
		"application/json",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("POST /withdraw: %v", err)
	}

	var receipt domain.WithdrawalReceipt
	decodeResponse(t, response, http.StatusOK, &receipt)
	if receipt.InitialAccount.Balance != 100 || receipt.FinalAccount.Balance != 60 {
		t.Errorf("balances = %v -> %v, want 100 -> 60", receipt.InitialAccount.Balance, receipt.FinalAccount.Balance)
	}
	if receipt.WithdrawalAmount != 40 {
		t.Errorf("withdrawal amount = %v, want 40", receipt.WithdrawalAmount)
	}

	assertE2EAccount(t, server, "acc1", 60, 2)
	assertE2ELastEvent(t, server, "acc1", 1, "MoneyDebited", 40)
}

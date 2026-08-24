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

func TestAuthenticationAndAccountAuthorizationEndToEnd(t *testing.T) {
	store := eventstore.NewMemoryStore()
	seedE2EAccount(t, store, "other-account", 999)
	auth := service.NewAuthService(store)
	server := httptest.NewServer(httpapi.NewRouter(httpapi.Dependencies{
		Store: store, OnboardingService: service.NewOnboardingService(store), AuthService: auth,
	}))
	t.Cleanup(server.Close)

	unauthorized, err := server.Client().Get(server.URL + "/accounts")
	if err != nil {
		t.Fatalf("GET /accounts: %v", err)
	}
	var unauthorizedBody map[string]string
	decodeResponse(t, unauthorized, http.StatusUnauthorized, &unauthorizedBody)

	payload := map[string]any{
		"legal_first_name": "Ada", "legal_last_name": "Lovelace", "date_of_birth": "1990-12-10",
		"nationality": "GB", "email": "ada@example.com", "phone": "+66812345678",
		"password": "correct-horse-battery-staple", "initial_deposit": 25,
		"residential_address": map[string]any{"line1": "1 Computing Lane", "city": "Bangkok", "postal_code": "10110", "country": "TH"},
		"government_document": map[string]any{"type": "passport", "number": "AUTH123", "issuing_country": "GB"},
	}
	var onboarded domain.OnboardingReceipt
	decodeResponse(t, postOnboarding(t, server, payload), http.StatusCreated, &onboarded)

	badLogin := postLogin(t, server, "ada@example.com", "wrong-password")
	var loginError map[string]string
	decodeResponse(t, badLogin, http.StatusUnauthorized, &loginError)

	loginResponse := postLogin(t, server, "ada@example.com", "correct-horse-battery-staple")
	var principal domain.Principal
	decodeResponse(t, loginResponse, http.StatusOK, &principal)
	if principal.AccountID != onboarded.AccountID {
		t.Fatalf("principal = %+v", principal)
	}
	cookies := loginResponse.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly {
		t.Fatalf("session cookie = %+v", cookies)
	}

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/accounts", nil)
	request.AddCookie(cookies[0])
	accountsResponse, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("authenticated GET /accounts: %v", err)
	}
	var accounts struct {
		Accounts []struct {
			ID string `json:"id"`
		} `json:"accounts"`
	}
	decodeResponse(t, accountsResponse, http.StatusOK, &accounts)
	if len(accounts.Accounts) != 1 || accounts.Accounts[0].ID != onboarded.AccountID {
		t.Fatalf("accounts = %+v", accounts)
	}

	forbiddenRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/accounts/other-account/events", nil)
	forbiddenRequest.AddCookie(cookies[0])
	forbiddenResponse, err := server.Client().Do(forbiddenRequest)
	if err != nil {
		t.Fatalf("forbidden request: %v", err)
	}
	var forbidden map[string]string
	decodeResponse(t, forbiddenResponse, http.StatusForbidden, &forbidden)
}

func postLogin(t *testing.T, server *httptest.Server, email, password string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	response, err := server.Client().Post(server.URL+"/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}
	return response
}

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
	hash, _ := service.HashPassword("admin-password-123")
	_ = store.UpsertAdmin(domain.Admin{ID: "adm_1", Email: "admin@example.com", PasswordHash: hash})
	server := httptest.NewServer(httpapi.NewRouter(httpapi.Dependencies{
		Store: store, OnboardingService: service.NewOnboardingService(store), AuthService: auth, ReviewService: service.NewReviewService(store),
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
	if principal.KYCStatus != domain.KYCWaiting || principal.AccountID != "" {
		t.Fatalf("principal = %+v", principal)
	}
	cookies := loginResponse.Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly {
		t.Fatalf("session cookie = %+v", cookies)
	}
	customerAdminRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/admin/applications", nil)
	customerAdminRequest.AddCookie(cookies[0])
	customerAdminResponse, _ := server.Client().Do(customerAdminRequest)
	var customerAdminError map[string]string
	decodeResponse(t, customerAdminResponse, http.StatusForbidden, &customerAdminError)

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/accounts", nil)
	request.AddCookie(cookies[0])
	accountsResponse, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("authenticated GET /accounts: %v", err)
	}
	var pendingError map[string]string
	decodeResponse(t, accountsResponse, http.StatusForbidden, &pendingError)

	adminLogin := postLogin(t, server, "admin@example.com", "admin-password-123")
	var admin domain.Principal
	decodeResponse(t, adminLogin, http.StatusOK, &admin)
	adminCookie := adminLogin.Cookies()[0]
	adminBankingRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/accounts", nil)
	adminBankingRequest.AddCookie(adminCookie)
	adminBankingResponse, _ := server.Client().Do(adminBankingRequest)
	var adminBankingError map[string]string
	decodeResponse(t, adminBankingResponse, http.StatusForbidden, &adminBankingError)
	passportRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/admin/applications/"+onboarded.CustomerID+"/passport", nil)
	passportRequest.AddCookie(adminCookie)
	passportResponse, _ := server.Client().Do(passportRequest)
	if passportResponse.StatusCode != http.StatusOK || passportResponse.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("passport status=%d type=%s", passportResponse.StatusCode, passportResponse.Header.Get("Content-Type"))
	}
	passportResponse.Body.Close()
	historyRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/admin/accounts/other-account/events", nil)
	historyRequest.AddCookie(adminCookie)
	historyResponse, _ := server.Client().Do(historyRequest)
	var adminHistory struct {
		AggregateID string `json:"aggregate_id"`
		EventCount  int    `json:"event_count"`
	}
	decodeResponse(t, historyResponse, http.StatusOK, &adminHistory)
	if adminHistory.AggregateID != "other-account" || adminHistory.EventCount != 1 {
		t.Fatalf("admin history=%+v", adminHistory)
	}
	approve, _ := http.NewRequest(http.MethodPost, server.URL+"/admin/applications/"+onboarded.CustomerID+"/approve", nil)
	approve.AddCookie(adminCookie)
	approvedResponse, err := server.Client().Do(approve)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approvedResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("approve status = %d", approvedResponse.StatusCode)
	}
	approvedResponse.Body.Close()

	request, _ = http.NewRequest(http.MethodGet, server.URL+"/accounts", nil)
	request.AddCookie(cookies[0])
	accountsResponse, err = server.Client().Do(request)
	if err != nil {
		t.Fatalf("approved GET /accounts: %v", err)
	}
	var accounts struct {
		Accounts []struct {
			ID string `json:"id"`
		} `json:"accounts"`
	}
	decodeResponse(t, accountsResponse, http.StatusOK, &accounts)
	if len(accounts.Accounts) != 1 || accounts.Accounts[0].ID == "" {
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

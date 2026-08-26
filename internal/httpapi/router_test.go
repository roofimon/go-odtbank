package httpapi

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
)

type stubDepositService struct {
	receipt *domain.DepositReceipt
	err     error
}

type stubWithdrawService struct {
	receipt *domain.WithdrawalReceipt
	err     error
}

type stubOnboardingService struct {
	receipt *domain.OnboardingReceipt
	err     error
}

func (s stubOnboardingService) Onboard(domain.OnboardingCommand) (*domain.OnboardingReceipt, error) {
	return s.receipt, s.err
}

func (s stubWithdrawService) Withdraw(domain.Money, string) (*domain.WithdrawalReceipt, error) {
	return s.receipt, s.err
}

func (s stubDepositService) Deposit(domain.Money, string) (*domain.DepositReceipt, error) {
	return s.receipt, s.err
}

func TestHandleDepositSuccess(t *testing.T) {
	svc := stubDepositService{receipt: &domain.DepositReceipt{
		InitialAccount: &domain.Account{ID: "acc1", Balance: 10000},
		FinalAccount:   &domain.Account{ID: "acc1", Balance: 11000},
		DepositAmount:  1000,
	}}
	req := httptest.NewRequest(http.MethodPost, "/deposit", strings.NewReader(`{"account_id":"acc1","amount":10}`))
	res := httptest.NewRecorder()
	handleDeposit(svc).ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"DepositAmount":10`) {
		t.Fatalf("body = %s", res.Body.String())
	}
}

func TestHandleDepositErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"invalid amount", domain.ErrInvalidDepositAmount, http.StatusBadRequest},
		{"missing account", domain.ErrAccountNotFound, http.StatusNotFound},
		{"conflict", eventstore.ErrConcurrencyConflict, http.StatusConflict},
		{"internal", errors.New("failed"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/deposit", strings.NewReader(`{"account_id":"acc1","amount":10}`))
			handleDeposit(stubDepositService{err: tt.err}).ServeHTTP(res, req)
			if res.Code != tt.want {
				t.Fatalf("status = %d, want %d", res.Code, tt.want)
			}
		})
	}
}

func TestHandleDepositRejectsInvalidJSON(t *testing.T) {
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/deposit", strings.NewReader(`{`))
	handleDeposit(stubDepositService{}).ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.Code)
	}
}

func TestHandleWithdrawSuccess(t *testing.T) {
	svc := stubWithdrawService{receipt: &domain.WithdrawalReceipt{
		InitialAccount:   &domain.Account{ID: "acc1", Balance: 10000},
		FinalAccount:     &domain.Account{ID: "acc1", Balance: 9000},
		WithdrawalAmount: 1000,
	}}
	req := httptest.NewRequest(http.MethodPost, "/withdraw", strings.NewReader(`{"account_id":"acc1","amount":10}`))
	res := httptest.NewRecorder()
	handleWithdraw(svc).ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"WithdrawalAmount":10`) {
		t.Fatalf("body = %s", res.Body.String())
	}
}

func TestHandleWithdrawErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"invalid amount", domain.ErrInvalidWithdrawAmount, http.StatusBadRequest},
		{"missing account", domain.ErrAccountNotFound, http.StatusNotFound},
		{"conflict", eventstore.ErrConcurrencyConflict, http.StatusConflict},
		{"insufficient funds", domain.NewInsufficientFundsError(&domain.Account{}, 10), http.StatusUnprocessableEntity},
		{"internal", errors.New("failed"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/withdraw", strings.NewReader(`{"account_id":"acc1","amount":10}`))
			handleWithdraw(stubWithdrawService{err: tt.err}).ServeHTTP(res, req)
			if res.Code != tt.want {
				t.Fatalf("status = %d, want %d", res.Code, tt.want)
			}
		})
	}
}

func TestHandleWithdrawRejectsInvalidJSON(t *testing.T) {
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/withdraw", strings.NewReader(`{`))
	handleWithdraw(stubWithdrawService{}).ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.Code)
	}
}

func TestHandleOnboardingSuccess(t *testing.T) {
	svc := stubOnboardingService{receipt: &domain.OnboardingReceipt{
		CustomerID: "cus_1", KYCStatus: domain.KYCWaiting,
	}}
	response := httptest.NewRecorder()
	request := onboardingMultipartRequest(t, `{"legal_first_name":"Ada"}`)
	handleOnboarding(svc).ServeHTTP(response, request)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"kyc_status":"waiting_for_approval"`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestHandleOnboardingValidationError(t *testing.T) {
	svc := stubOnboardingService{err: &domain.OnboardingValidationError{Field: "email", Message: "email must be valid"}}
	response := httptest.NewRecorder()
	request := onboardingMultipartRequest(t, `{}`)
	handleOnboarding(svc).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"field":"email"`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestHandleOnboardingRejectsInvalidJSON(t *testing.T) {
	response := httptest.NewRecorder()
	request := onboardingMultipartRequest(t, `{`)
	handleOnboarding(stubOnboardingService{}).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestHandleOnboardingInternalError(t *testing.T) {
	response := httptest.NewRecorder()
	request := onboardingMultipartRequest(t, `{}`)
	handleOnboarding(stubOnboardingService{err: errors.New("failed")}).ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
}

func onboardingMultipartRequest(t *testing.T, payload string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("payload", payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	image, err := writer.CreateFormFile("passport_image", "passport.png")
	if err != nil {
		t.Fatalf("create image part: %v", err)
	}
	if _, err := image.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}); err != nil {
		t.Fatalf("write image: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/onboarding", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestCORSPreflightAllowsTransferIdempotencyKey(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/transfer", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "content-type,idempotency-key")
	withCORS(http.NotFoundHandler(), "http://localhost:3000").ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(strings.ToLower(got), "idempotency-key") {
		t.Fatalf("allowed headers = %q", got)
	}
}

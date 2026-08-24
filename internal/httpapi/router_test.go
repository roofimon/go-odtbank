package httpapi

import (
	"errors"
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

func (s stubWithdrawService) Withdraw(float64, string) (*domain.WithdrawalReceipt, error) {
	return s.receipt, s.err
}

func (s stubDepositService) Deposit(float64, string) (*domain.DepositReceipt, error) {
	return s.receipt, s.err
}

func TestHandleDepositSuccess(t *testing.T) {
	svc := stubDepositService{receipt: &domain.DepositReceipt{
		InitialAccount: &domain.Account{ID: "acc1", Balance: 100},
		FinalAccount:   &domain.Account{ID: "acc1", Balance: 110},
		DepositAmount:  10,
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
		InitialAccount:   &domain.Account{ID: "acc1", Balance: 100},
		FinalAccount:     &domain.Account{ID: "acc1", Balance: 90},
		WithdrawalAmount: 10,
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

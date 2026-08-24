package domain

import (
	"errors"
	"time"
)

type Session struct {
	TokenHash  string
	CustomerID string
	AdminID    string
	ExpiresAt  time.Time
}

type Principal struct {
	CustomerID      string `json:"customer_id,omitempty"`
	AccountID       string `json:"account_id,omitempty"`
	AdminID         string `json:"admin_id,omitempty"`
	Email           string `json:"email"`
	Role            string `json:"role"`
	KYCStatus       string `json:"kyc_status,omitempty"`
	RejectionReason string `json:"rejection_reason,omitempty"`
}

type Admin struct{ ID, Email, PasswordHash string }

type AuthStore interface {
	FindCustomerByEmail(email string) (*Customer, error)
	FindAdminByEmail(email string) (*Admin, error)
	UpsertAdmin(admin Admin) error
	CreateSession(session Session) error
	FindSession(tokenHash string, now time.Time) (*Principal, error)
	DeleteSession(tokenHash string) error
}

type AuthService interface {
	Login(email, password string) (token string, principal *Principal, err error)
	Authenticate(token string) (*Principal, error)
	Logout(token string) error
}

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUnauthorized       = errors.New("authentication required")
	ErrForbidden          = errors.New("access denied")
)

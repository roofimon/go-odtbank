package domain

import (
	"errors"
	"time"
)

type Session struct {
	TokenHash  string
	CustomerID string
	AccountID  string
	ExpiresAt  time.Time
}

type Principal struct {
	CustomerID string `json:"customer_id"`
	AccountID  string `json:"account_id"`
	Email      string `json:"email"`
}

type AuthStore interface {
	FindCustomerByEmail(email string) (*Customer, error)
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

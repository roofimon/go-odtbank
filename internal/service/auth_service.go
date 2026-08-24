package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"go-odtbank/internal/domain"
)

type AuthService struct{ store domain.AuthStore }

func NewAuthService(store domain.AuthStore) *AuthService { return &AuthService{store: store} }

func (s *AuthService) Login(email, password string) (string, *domain.Principal, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var session domain.Session
	var principal *domain.Principal
	if admin, err := s.store.FindAdminByEmail(email); err == nil && verifyPassword(admin.PasswordHash, password) {
		session.AdminID = admin.ID
		principal = &domain.Principal{AdminID: admin.ID, Email: admin.Email, Role: "admin"}
	} else {
		customer, err := s.store.FindCustomerByEmail(email)
		if err != nil || !verifyPassword(customer.PasswordHash, password) {
			return "", nil, domain.ErrInvalidCredentials
		}
		session.CustomerID = customer.ID
		principal = &domain.Principal{CustomerID: customer.ID, AccountID: customer.AccountID, Email: customer.Email, Role: "customer", KYCStatus: customer.KYCStatus, RejectionReason: customer.RejectionReason}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	session.TokenHash = tokenHash(token)
	session.ExpiresAt = time.Now().UTC().Add(24 * time.Hour)
	if err := s.store.CreateSession(session); err != nil {
		return "", nil, err
	}
	return token, principal, nil
}

func (s *AuthService) Authenticate(token string) (*domain.Principal, error) {
	if token == "" {
		return nil, domain.ErrUnauthorized
	}
	principal, err := s.store.FindSession(tokenHash(token), time.Now().UTC())
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	return principal, nil
}

func (s *AuthService) Logout(token string) error {
	if token == "" {
		return nil
	}
	return s.store.DeleteSession(tokenHash(token))
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

var _ domain.AuthService = (*AuthService)(nil)

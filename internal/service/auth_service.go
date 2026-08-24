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
	customer, err := s.store.FindCustomerByEmail(strings.ToLower(strings.TrimSpace(email)))
	if err != nil || !verifyPassword(customer.PasswordHash, password) {
		return "", nil, domain.ErrInvalidCredentials
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	session := domain.Session{TokenHash: tokenHash(token), CustomerID: customer.ID, AccountID: customer.AccountID, ExpiresAt: time.Now().UTC().Add(24 * time.Hour)}
	if err := s.store.CreateSession(session); err != nil {
		return "", nil, err
	}
	return token, &domain.Principal{CustomerID: customer.ID, AccountID: customer.AccountID, Email: customer.Email}, nil
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

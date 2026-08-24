package domain

import (
	"errors"
	"time"
)

type ResidentialAddress struct {
	Line1           string
	Line2           string
	City            string
	StateOrProvince string
	PostalCode      string
	Country         string
}

type GovernmentDocument struct {
	Type           string
	Number         string
	IssuingCountry string
}

type OnboardingCommand struct {
	LegalFirstName     string
	LegalLastName      string
	DateOfBirth        string
	Nationality        string
	Email              string
	Phone              string
	Password           string
	ResidentialAddress ResidentialAddress
	GovernmentDocument GovernmentDocument
	PassportImage      []byte
	InitialDeposit     float64
}

type Customer struct {
	ID                 string
	AccountID          string
	LegalFirstName     string
	LegalLastName      string
	DateOfBirth        time.Time
	Nationality        string
	Email              string
	Phone              string
	PasswordHash       string
	ResidentialAddress ResidentialAddress
	GovernmentDocument GovernmentDocument
	PassportImage      []byte
	PassportImageMIME  string
	KYCStatus          string
	CreatedAt          time.Time
}

type OnboardingReceipt struct {
	CustomerID string  `json:"customer_id"`
	AccountID  string  `json:"account_id"`
	KYCStatus  string  `json:"kyc_status"`
	Balance    float64 `json:"balance"`
}

type OnboardingService interface {
	Onboard(command OnboardingCommand) (*OnboardingReceipt, error)
}

type OnboardingStore interface {
	CreateCustomerAccount(customer Customer, opened AccountOpened) error
}

var (
	ErrInvalidOnboarding     = errors.New("invalid onboarding data")
	ErrCustomerAlreadyExists = errors.New("customer already exists")
)

type OnboardingValidationError struct {
	Field   string
	Message string
}

func (e *OnboardingValidationError) Error() string { return e.Message }

func (e *OnboardingValidationError) Unwrap() error { return ErrInvalidOnboarding }

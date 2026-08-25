package domain

import (
	"errors"
	"time"
)

type ResidentialAddress struct {
	Line1           string `json:"line1"`
	Line2           string `json:"line2"`
	City            string `json:"city"`
	StateOrProvince string `json:"state_or_province"`
	PostalCode      string `json:"postal_code"`
	Country         string `json:"country"`
}

type GovernmentDocument struct {
	Type           string `json:"type"`
	Number         string `json:"number"`
	IssuingCountry string `json:"issuing_country"`
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
	InitialDeposit     Money
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
	RequestedDeposit   Money
	ReviewedBy         string
	ReviewedAt         *time.Time
	RejectionReason    string
	CreatedAt          time.Time
}

type OnboardingReceipt struct {
	CustomerID string `json:"customer_id"`
	KYCStatus  string `json:"kyc_status"`
}

type OnboardingService interface {
	Onboard(command OnboardingCommand) (*OnboardingReceipt, error)
}

type OnboardingStore interface {
	CreateCustomerApplication(customer Customer) error
}

const (
	KYCWaiting  = "waiting_for_approval"
	KYCApproved = "approved"
	KYCRejected = "rejected"
)

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

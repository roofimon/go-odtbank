package domain

import (
	"errors"
	"time"
)

type ApplicationSummary struct {
	CustomerID       string     `json:"customer_id"`
	LegalFirstName   string     `json:"legal_first_name"`
	LegalLastName    string     `json:"legal_last_name"`
	Email            string     `json:"email"`
	KYCStatus        string     `json:"kyc_status"`
	RequestedDeposit Money      `json:"requested_initial_deposit"`
	CreatedAt        time.Time  `json:"created_at"`
	ReviewedAt       *time.Time `json:"reviewed_at,omitempty"`
	RejectionReason  string     `json:"rejection_reason,omitempty"`
}

type ApplicationDetail struct {
	ApplicationSummary
	DateOfBirth        time.Time          `json:"date_of_birth"`
	Nationality        string             `json:"nationality"`
	Phone              string             `json:"phone"`
	ResidentialAddress ResidentialAddress `json:"residential_address"`
	GovernmentDocument GovernmentDocument `json:"government_document"`
	PassportImageMIME  string             `json:"passport_image_mime"`
}

type ReviewStore interface {
	ListApplications(status string) ([]ApplicationSummary, error)
	GetApplication(customerID string) (*ApplicationDetail, error)
	GetPassportImage(customerID string) ([]byte, string, error)
	ApproveApplication(customerID, adminID, accountID string, reviewedAt time.Time) error
	RejectApplication(customerID, adminID, reason string, reviewedAt time.Time) error
}

type ReviewService interface {
	List(status string) ([]ApplicationSummary, error)
	Get(customerID string) (*ApplicationDetail, error)
	Passport(customerID string) ([]byte, string, error)
	Approve(customerID, adminID string) error
	Reject(customerID, adminID, reason string) error
}

var (
	ErrApplicationNotFound    = errors.New("application not found")
	ErrInvalidReviewStatus    = errors.New("invalid application status")
	ErrApplicationReviewed    = errors.New("application has already been reviewed")
	ErrInvalidRejectionReason = errors.New("rejection reason must contain 1 to 500 characters")
)

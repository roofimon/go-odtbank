package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"go-odtbank/internal/domain"
)

var (
	countryCodePattern = regexp.MustCompile(`^[A-Z]{2}$`)
	phonePattern       = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
)

type OnboardingService struct {
	store domain.OnboardingStore
}

func NewOnboardingService(store domain.OnboardingStore) *OnboardingService {
	return &OnboardingService{store: store}
}

func (s *OnboardingService) Onboard(command domain.OnboardingCommand) (*domain.OnboardingReceipt, error) {
	command = normalizeOnboarding(command)
	birthDate, err := validateOnboarding(command, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	customerID, err := randomID("cus_")
	if err != nil {
		return nil, fmt.Errorf("generate customer id: %w", err)
	}
	accountID, err := randomID("acc_")
	if err != nil {
		return nil, fmt.Errorf("generate account id: %w", err)
	}
	now := time.Now().UTC()
	customer := domain.Customer{
		ID: customerID, AccountID: accountID,
		LegalFirstName: command.LegalFirstName, LegalLastName: command.LegalLastName,
		DateOfBirth: birthDate, Nationality: command.Nationality,
		Email: command.Email, Phone: command.Phone,
		ResidentialAddress: command.ResidentialAddress,
		GovernmentDocument: command.GovernmentDocument,
		PassportImage:      command.PassportImage,
		PassportImageMIME:  http.DetectContentType(command.PassportImage),
		KYCStatus:          "verified", CreatedAt: now,
	}
	opened := domain.AccountOpened{
		Aggregate: accountID, Type: "AccountOpened", Seq: 0, Occurred: now,
		ID: accountID, InitialBalance: command.InitialDeposit,
	}
	if err := s.store.CreateCustomerAccount(customer, opened); err != nil {
		return nil, err
	}
	return &domain.OnboardingReceipt{
		CustomerID: customerID, AccountID: accountID,
		KYCStatus: "verified", Balance: command.InitialDeposit,
	}, nil
}

func normalizeOnboarding(command domain.OnboardingCommand) domain.OnboardingCommand {
	command.LegalFirstName = strings.TrimSpace(command.LegalFirstName)
	command.LegalLastName = strings.TrimSpace(command.LegalLastName)
	command.DateOfBirth = strings.TrimSpace(command.DateOfBirth)
	command.Nationality = strings.ToUpper(strings.TrimSpace(command.Nationality))
	command.Email = strings.ToLower(strings.TrimSpace(command.Email))
	command.Phone = strings.TrimSpace(command.Phone)
	command.ResidentialAddress.Line1 = strings.TrimSpace(command.ResidentialAddress.Line1)
	command.ResidentialAddress.Line2 = strings.TrimSpace(command.ResidentialAddress.Line2)
	command.ResidentialAddress.City = strings.TrimSpace(command.ResidentialAddress.City)
	command.ResidentialAddress.StateOrProvince = strings.TrimSpace(command.ResidentialAddress.StateOrProvince)
	command.ResidentialAddress.PostalCode = strings.TrimSpace(command.ResidentialAddress.PostalCode)
	command.ResidentialAddress.Country = strings.ToUpper(strings.TrimSpace(command.ResidentialAddress.Country))
	command.GovernmentDocument.Type = strings.ToLower(strings.TrimSpace(command.GovernmentDocument.Type))
	command.GovernmentDocument.Number = strings.ToUpper(strings.TrimSpace(command.GovernmentDocument.Number))
	command.GovernmentDocument.IssuingCountry = strings.ToUpper(strings.TrimSpace(command.GovernmentDocument.IssuingCountry))
	return command
}

func validateOnboarding(command domain.OnboardingCommand, now time.Time) (time.Time, error) {
	required := []struct{ field, value string }{
		{"legal_first_name", command.LegalFirstName}, {"legal_last_name", command.LegalLastName},
		{"date_of_birth", command.DateOfBirth}, {"nationality", command.Nationality},
		{"email", command.Email}, {"phone", command.Phone},
		{"residential_address.line1", command.ResidentialAddress.Line1},
		{"residential_address.city", command.ResidentialAddress.City},
		{"residential_address.postal_code", command.ResidentialAddress.PostalCode},
		{"residential_address.country", command.ResidentialAddress.Country},
		{"government_document.type", command.GovernmentDocument.Type},
		{"government_document.number", command.GovernmentDocument.Number},
		{"government_document.issuing_country", command.GovernmentDocument.IssuingCountry},
	}
	for _, item := range required {
		if item.value == "" {
			return time.Time{}, validationError(item.field, "is required")
		}
		if len(item.value) > 200 {
			return time.Time{}, validationError(item.field, "is too long")
		}
	}
	if _, err := mail.ParseAddress(command.Email); err != nil {
		return time.Time{}, validationError("email", "must be a valid email address")
	}
	if !phonePattern.MatchString(command.Phone) {
		return time.Time{}, validationError("phone", "must use E.164 format")
	}
	for field, value := range map[string]string{
		"nationality":                         command.Nationality,
		"residential_address.country":         command.ResidentialAddress.Country,
		"government_document.issuing_country": command.GovernmentDocument.IssuingCountry,
	} {
		if !countryCodePattern.MatchString(value) {
			return time.Time{}, validationError(field, "must be a two-letter country code")
		}
	}
	validDocument := map[string]bool{"passport": true, "national_id": true, "driver_license": true}
	if !validDocument[command.GovernmentDocument.Type] {
		return time.Time{}, validationError("government_document.type", "is not supported")
	}
	birthDate, err := time.Parse("2006-01-02", command.DateOfBirth)
	if err != nil || birthDate.After(now) {
		return time.Time{}, validationError("date_of_birth", "must be a valid past date")
	}
	age := now.Year() - birthDate.Year()
	birthdayThisYear := time.Date(now.Year(), birthDate.Month(), birthDate.Day(), 0, 0, 0, 0, time.UTC)
	if now.Before(birthdayThisYear) {
		age--
	}
	if age < 18 {
		return time.Time{}, validationError("date_of_birth", "customer must be at least 18")
	}
	if math.IsNaN(command.InitialDeposit) || math.IsInf(command.InitialDeposit, 0) || command.InitialDeposit < 0 || (command.InitialDeposit > 0 && command.InitialDeposit < 10) {
		return time.Time{}, validationError("initial_deposit", "must be zero or at least 10.00")
	}
	if len(command.PassportImage) == 0 {
		return time.Time{}, validationError("passport_image", "is required")
	}
	if len(command.PassportImage) > 5<<20 {
		return time.Time{}, validationError("passport_image", "must not exceed 5 MB")
	}
	detectedMIME := http.DetectContentType(command.PassportImage)
	allowedMIME := map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}
	if !allowedMIME[detectedMIME] {
		return time.Time{}, validationError("passport_image", "must be a JPEG, PNG, or WebP image")
	}
	return birthDate, nil
}

func validationError(field, message string) error {
	return &domain.OnboardingValidationError{Field: field, Message: field + " " + message}
}

func randomID(prefix string) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(bytes), nil
}

var _ domain.OnboardingService = (*OnboardingService)(nil)

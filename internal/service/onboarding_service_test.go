package service_test

import (
	"errors"
	"testing"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/service"
)

func validOnboardingCommand() domain.OnboardingCommand {
	return domain.OnboardingCommand{
		LegalFirstName: "Ada", LegalLastName: "Lovelace",
		DateOfBirth: "1990-12-10", Nationality: "GB",
		Email: "ada@example.com", Phone: "+66812345678",
		Password: "correct-horse-battery-staple",
		ResidentialAddress: domain.ResidentialAddress{
			Line1: "1 Computing Lane", City: "Bangkok", PostalCode: "10110", Country: "TH",
		},
		GovernmentDocument: domain.GovernmentDocument{
			Type: "passport", Number: "P123456", IssuingCountry: "GB",
		},
		PassportImage:  []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
		InitialDeposit: 2500,
	}
}

func TestOnboarding_CreatesWaitingApplicationWithoutAccount(t *testing.T) {
	store := eventstore.NewMemoryStore()
	receipt, err := service.NewOnboardingService(store).Onboard(validOnboardingCommand())
	if err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	if receipt.CustomerID == "" || receipt.KYCStatus != domain.KYCWaiting {
		t.Fatalf("receipt = %+v", receipt)
	}
	ids, _ := store.ListAggregates()
	if len(ids) != 0 {
		t.Fatalf("aggregates = %v, want none", ids)
	}
}

func TestOnboarding_AllowsZeroInitialDeposit(t *testing.T) {
	command := validOnboardingCommand()
	command.InitialDeposit = 0
	receipt, err := service.NewOnboardingService(eventstore.NewMemoryStore()).Onboard(command)
	if err != nil || receipt.KYCStatus != domain.KYCWaiting {
		t.Fatalf("receipt = %+v, err = %v", receipt, err)
	}
}

func TestOnboarding_RejectsInvalidCommands(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*domain.OnboardingCommand)
		field  string
	}{
		{"underage", func(c *domain.OnboardingCommand) { c.DateOfBirth = "2015-01-01" }, "date_of_birth"},
		{"bad email", func(c *domain.OnboardingCommand) { c.Email = "invalid" }, "email"},
		{"bad phone", func(c *domain.OnboardingCommand) { c.Phone = "0812345678" }, "phone"},
		{"bad country", func(c *domain.OnboardingCommand) { c.Nationality = "GBR" }, "nationality"},
		{"bad document", func(c *domain.OnboardingCommand) { c.GovernmentDocument.Type = "library_card" }, "government_document.type"},
		{"small funding", func(c *domain.OnboardingCommand) { c.InitialDeposit = 999 }, "initial_deposit"},
		{"missing passport", func(c *domain.OnboardingCommand) { c.PassportImage = nil }, "passport_image"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := validOnboardingCommand()
			tt.mutate(&command)
			_, err := service.NewOnboardingService(eventstore.NewMemoryStore()).Onboard(command)
			var validationErr *domain.OnboardingValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.field {
				t.Fatalf("error = %v, want validation field %s", err, tt.field)
			}
		})
	}
}

func TestOnboarding_RejectsDuplicateDocumentWithoutAccount(t *testing.T) {
	store := eventstore.NewMemoryStore()
	svc := service.NewOnboardingService(store)
	if _, err := svc.Onboard(validOnboardingCommand()); err != nil {
		t.Fatalf("first onboarding: %v", err)
	}
	command := validOnboardingCommand()
	command.Email = "other@example.com"
	command.GovernmentDocument.Type = " PASSPORT "
	command.GovernmentDocument.Number = " p123456 "
	command.GovernmentDocument.IssuingCountry = " gb "
	receipt, err := svc.Onboard(command)
	if !errors.Is(err, domain.ErrCustomerAlreadyExists) || receipt != nil {
		t.Fatalf("receipt = %+v, err = %v", receipt, err)
	}
	ids, _ := store.ListAggregates()
	if len(ids) != 0 {
		t.Fatalf("aggregate count = %d, want 0", len(ids))
	}
}

type failingOnboardingStore struct{ err error }

func (s failingOnboardingStore) CreateCustomerApplication(domain.Customer) error {
	return s.err
}

func TestOnboarding_PropagatesPersistenceFailure(t *testing.T) {
	want := errors.New("storage unavailable")
	_, err := service.NewOnboardingService(failingOnboardingStore{err: want}).Onboard(validOnboardingCommand())
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

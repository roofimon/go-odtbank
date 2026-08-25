package service_test

import (
	"errors"
	"testing"

	"go-odtbank/internal/domain"
	"go-odtbank/internal/eventstore"
	"go-odtbank/internal/service"
)

func TestReviewApprovalCreatesFundedAccountOnce(t *testing.T) {
	store := eventstore.NewMemoryStore()
	receipt, err := service.NewOnboardingService(store).Onboard(validOnboardingCommand())
	if err != nil {
		t.Fatal(err)
	}
	review := service.NewReviewService(store)
	if err := review.Approve(receipt.CustomerID, "adm_1"); err != nil {
		t.Fatal(err)
	}
	application, err := review.Get(receipt.CustomerID)
	if err != nil || application.KYCStatus != domain.KYCApproved {
		t.Fatalf("application=%+v err=%v", application, err)
	}
	applications, _ := review.List(domain.KYCApproved)
	if len(applications) != 1 {
		t.Fatalf("approved applications=%d", len(applications))
	}
	ids, _ := store.ListAggregates()
	if len(ids) != 1 {
		t.Fatalf("accounts=%v", ids)
	}
	events, _ := store.Load(ids[0])
	if got := domain.ReplayAccount(ids[0], events).Balance; got != 2500 {
		t.Fatalf("balance=%v", got)
	}
	if err := review.Approve(receipt.CustomerID, "adm_1"); !errors.Is(err, domain.ErrApplicationReviewed) {
		t.Fatalf("second approval=%v", err)
	}
}

func TestReviewRejectionIsFinalAndRequiresReason(t *testing.T) {
	store := eventstore.NewMemoryStore()
	receipt, _ := service.NewOnboardingService(store).Onboard(validOnboardingCommand())
	review := service.NewReviewService(store)
	if err := review.Reject(receipt.CustomerID, "adm_1", " "); !errors.Is(err, domain.ErrInvalidRejectionReason) {
		t.Fatalf("empty reason=%v", err)
	}
	if err := review.Reject(receipt.CustomerID, "adm_1", "Passport image is unreadable"); err != nil {
		t.Fatal(err)
	}
	application, _ := review.Get(receipt.CustomerID)
	if application.KYCStatus != domain.KYCRejected || application.RejectionReason == "" {
		t.Fatalf("application=%+v", application)
	}
	if err := review.Approve(receipt.CustomerID, "adm_1"); !errors.Is(err, domain.ErrApplicationReviewed) {
		t.Fatalf("approve rejected=%v", err)
	}
	ids, _ := store.ListAggregates()
	if len(ids) != 0 {
		t.Fatalf("accounts=%v", ids)
	}
}

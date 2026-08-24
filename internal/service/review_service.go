package service

import (
	"strings"
	"time"

	"go-odtbank/internal/domain"
)

type ReviewService struct{ store domain.ReviewStore }

func NewReviewService(store domain.ReviewStore) *ReviewService { return &ReviewService{store: store} }

func (s *ReviewService) List(status string) ([]domain.ApplicationSummary, error) {
	if status == "" {
		status = domain.KYCWaiting
	}
	if status != domain.KYCWaiting && status != domain.KYCApproved && status != domain.KYCRejected {
		return nil, domain.ErrInvalidReviewStatus
	}
	return s.store.ListApplications(status)
}
func (s *ReviewService) Get(id string) (*domain.ApplicationDetail, error) {
	return s.store.GetApplication(id)
}
func (s *ReviewService) Passport(id string) ([]byte, string, error) {
	return s.store.GetPassportImage(id)
}
func (s *ReviewService) Approve(id, adminID string) error {
	accountID, err := randomID("acc_")
	if err != nil {
		return err
	}
	return s.store.ApproveApplication(id, adminID, accountID, time.Now().UTC())
}
func (s *ReviewService) Reject(id, adminID, reason string) error {
	reason = strings.TrimSpace(reason)
	if len(reason) == 0 || len(reason) > 500 {
		return domain.ErrInvalidRejectionReason
	}
	return s.store.RejectApplication(id, adminID, reason, time.Now().UTC())
}

var _ domain.ReviewService = (*ReviewService)(nil)

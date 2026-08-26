package service

import (
	"go-odtbank/internal/domain"
	"strings"
	"time"
)

type AdjustmentService struct{ store domain.AdjustmentStore }

func NewAdjustmentService(store domain.AdjustmentStore) *AdjustmentService {
	return &AdjustmentService{store: store}
}
func (s *AdjustmentService) Create(r domain.AdjustmentRequest, adminID string) (*domain.AdjustmentRequest, error) {
	r.Type = strings.TrimSpace(r.Type)
	r.Direction = strings.TrimSpace(r.Direction)
	r.Reason = strings.TrimSpace(r.Reason)
	r.CaseReference = strings.TrimSpace(r.CaseReference)
	if (r.Type != domain.AdjustmentManual && r.Type != domain.AdjustmentReversal) || len(r.Reason) < 10 || len(r.Reason) > 500 || len(r.CaseReference) < 1 || len(r.CaseReference) > 100 {
		return nil, domain.ErrInvalidAdjustment
	}
	if r.Type == domain.AdjustmentManual && (r.AccountID == "" || (r.Direction != "credit" && r.Direction != "debit") || r.Amount < 1) {
		return nil, domain.ErrInvalidAdjustment
	}
	if r.Type == domain.AdjustmentReversal && r.OriginalTransferID == "" && (r.OriginalAccountID == "" || r.OriginalEventSequence == nil) {
		return nil, domain.ErrInvalidAdjustment
	}
	id, err := randomID("adj_")
	if err != nil {
		return nil, err
	}
	r.ID = id
	r.Status = domain.AdjustmentWaiting
	r.CreatedBy = adminID
	r.CreatedAt = time.Now().UTC()
	return s.store.CreateAdjustment(r)
}
func (s *AdjustmentService) List(status string) ([]domain.AdjustmentRequest, error) {
	if status == "" {
		status = domain.AdjustmentWaiting
	}
	if status != domain.AdjustmentWaiting && status != domain.AdjustmentApproved && status != domain.AdjustmentRejected {
		return nil, domain.ErrInvalidAdjustment
	}
	return s.store.ListAdjustments(status)
}
func (s *AdjustmentService) Get(id string) (*domain.AdjustmentRequest, error) {
	return s.store.GetAdjustment(id)
}
func (s *AdjustmentService) Approve(id, adminID string) error {
	return s.store.ApproveAdjustment(id, adminID, time.Now().UTC())
}
func (s *AdjustmentService) Reject(id, adminID, reason string) error {
	reason = strings.TrimSpace(reason)
	if len(reason) < 1 || len(reason) > 500 {
		return domain.ErrInvalidAdjustment
	}
	return s.store.RejectAdjustment(id, adminID, reason, time.Now().UTC())
}

var _ domain.AdjustmentService = (*AdjustmentService)(nil)

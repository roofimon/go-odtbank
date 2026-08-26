package domain

import (
	"errors"
	"time"
)

const (
	AdjustmentWaiting  = "waiting_for_approval"
	AdjustmentApproved = "approved"
	AdjustmentRejected = "rejected"
	AdjustmentManual   = "manual"
	AdjustmentReversal = "reversal"
)

type AdjustmentRequest struct {
	ID                    string     `json:"adjustment_id"`
	Type                  string     `json:"type"`
	Status                string     `json:"status"`
	AccountID             string     `json:"account_id"`
	Direction             string     `json:"direction,omitempty"`
	Amount                Money      `json:"amount"`
	Fee                   Money      `json:"fee,omitempty"`
	CounterpartyAccountID string     `json:"counterparty_account_id,omitempty"`
	OriginalTransferID    string     `json:"original_transfer_id,omitempty"`
	OriginalAccountID     string     `json:"original_account_id,omitempty"`
	OriginalEventSequence *int       `json:"original_event_sequence,omitempty"`
	Reason                string     `json:"reason"`
	CaseReference         string     `json:"case_reference"`
	CreatedBy             string     `json:"created_by"`
	ReviewedBy            string     `json:"reviewed_by,omitempty"`
	RejectionReason       string     `json:"rejection_reason,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	ReviewedAt            *time.Time `json:"reviewed_at,omitempty"`
}

type AdjustmentStore interface {
	CreateAdjustment(request AdjustmentRequest) (*AdjustmentRequest, error)
	ListAdjustments(status string) ([]AdjustmentRequest, error)
	GetAdjustment(id string) (*AdjustmentRequest, error)
	ApproveAdjustment(id, adminID string, at time.Time) error
	RejectAdjustment(id, adminID, reason string, at time.Time) error
}
type AdjustmentService interface {
	Create(request AdjustmentRequest, adminID string) (*AdjustmentRequest, error)
	List(status string) ([]AdjustmentRequest, error)
	Get(id string) (*AdjustmentRequest, error)
	Approve(id, adminID string) error
	Reject(id, adminID, reason string) error
}

var (
	ErrInvalidAdjustment  = errors.New("invalid adjustment request")
	ErrAdjustmentNotFound = errors.New("adjustment request not found")
	ErrAdjustmentReviewed = errors.New("adjustment request has already been reviewed")
	ErrSelfApproval       = errors.New("adjustment maker cannot approve their own request")
	ErrAlreadyReversed    = errors.New("transaction has already been reversed")
)

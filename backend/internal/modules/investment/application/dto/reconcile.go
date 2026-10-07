package dto

import (
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type ReconcileRequest struct {
	Scope     domain.Scope
	AccountID string
	Through   time.Time
}
type ReconcileResult struct {
	AccountID, State string
	AppliedEvents    int
	Blocked          []domain.Exclusion
}
type ActionRecord struct {
	Action      domain.CorporateAction `json:"action"`
	Eligibility []domain.Position      `json:"eligibility"`
}

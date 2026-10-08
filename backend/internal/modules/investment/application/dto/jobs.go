package dto

import (
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type InvestmentJobArgs struct {
	JobType, WorkspaceID, OwnerUserID, AccountID, RunID string
	SessionDate                                         time.Time
}

func (InvestmentJobArgs) Kind() string { return "investment_job" }

type AccountRef struct {
	Scope     domain.Scope
	AccountID string
}
type RunView struct {
	ID        string    `json:"runId"`
	Status    string    `json:"status"`
	Phase     string    `json:"phase"`
	ErrorCode string    `json:"errorCode"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type EvaluateRequest struct {
	Mutation
	Scope        domain.Scope `json:"-"`
	AccountID    string       `json:"-"`
	Purpose      string       `json:"purpose"`
	RunID        string       `json:"-"`
	SessionDate  time.Time    `json:"-"`
	FinalAttempt bool         `json:"-"`
}
type EvaluationView struct{ domain.Evaluation }
type EvaluationReadView struct {
	ID, AccountID, SnapshotID, DatasetVersion, Mode, StrategyVersionID, UniverseVersionID, State, Reason, Purpose string
	AsOf, SessionDate                                                                                             time.Time
	Signals                                                                                                       []domain.Signal
	Excluded                                                                                                      []domain.Exclusion
	QualityFlags                                                                                                  []string
	OrderIDs                                                                                                      []string
	IssuedOrders                                                                                                  bool
}

func ViewEvaluation(v domain.Evaluation) EvaluationReadView {
	return EvaluationReadView{ID: v.ID, AccountID: v.AccountID, SnapshotID: v.SnapshotID, DatasetVersion: v.DatasetVersion, Mode: v.Mode, StrategyVersionID: v.StrategyVersionID, UniverseVersionID: v.UniverseVersionID, State: v.State, Reason: v.Reason, Purpose: v.Purpose, AsOf: v.AsOf, SessionDate: v.SessionDate, Signals: v.Signals, Excluded: v.Excluded, QualityFlags: v.QualityFlags, OrderIDs: v.OrderIDs, IssuedOrders: v.IssuedOrders}
}

type EvaluationClaim struct {
	Found      bool
	Evaluation domain.Evaluation
}
type SyncRun struct {
	Scope   domain.Scope
	Request SyncRequest
	View    RunView
}

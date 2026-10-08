package dto

import (
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type CreateBacktestRequest struct {
	Mutation
	StrategyVersionID string    `json:"strategyVersionId"`
	UniverseVersionID string    `json:"universeVersionId"`
	InitialCash       string    `json:"initialCash"`
	From              time.Time `json:"from"`
	To                time.Time `json:"to"`
}
type RunBacktestRequest struct {
	Scope        domain.Scope
	RunID        string
	FinalAttempt bool
}
type BacktestRecord struct {
	Request              CreateBacktestRequest
	DatasetVersion, Mode string
	SnapshotIDs          []string
	View                 RunView
	Version              int
	Result               *domain.BacktestResult
}

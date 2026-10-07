package dto

import (
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
)

type Mutation struct {
	Scope           domain.Scope `json:"-"`
	Route, Key      string       `json:"-"`
	ExpectedVersion int          `json:"expectedVersion,omitempty"`
}
type CreateAccountRequest struct {
	Mutation
	Name              string `json:"name"`
	InitialCash       string `json:"initialCash"`
	Mode              string `json:"mode"`
	StrategyVersionID string `json:"strategyVersionId"`
	UniverseVersionID string `json:"universeVersionId"`
}
type CreateUniverseVersionRequest struct {
	Mutation
	UniverseID    string   `json:"universeId,omitempty"`
	Name          string   `json:"name"`
	Mode          string   `json:"mode"`
	InstrumentIDs []string `json:"instrumentIds"`
}
type CreateStrategyVersionRequest struct {
	Mutation
	StrategyID string                    `json:"strategyId"`
	Parameters domain.StrategyParameters `json:"parameters"`
}
type ConfigureAutomationRequest struct {
	Mutation
	AccountID         string            `json:"-"`
	Enabled           bool              `json:"enabled"`
	Mode              string            `json:"mode,omitempty"`
	StrategyVersionID string            `json:"strategyVersionId"`
	UniverseVersionID string            `json:"universeVersionId"`
	Policy            domain.RiskPolicy `json:"policy"`
}

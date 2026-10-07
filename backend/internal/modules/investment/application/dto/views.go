package dto

import (
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type CashView struct {
	Available string `json:"available"`
	Reserved  string `json:"reserved"`
	Unsettled string `json:"unsettled"`
	Dividends string `json:"dividends"`
}
type AccountView struct {
	DatasetVersion    string                `json:"datasetVersion"`
	ID                string                `json:"id"`
	Name              string                `json:"name"`
	Mode              string                `json:"mode"`
	StrategyVersionID string                `json:"strategyVersionId"`
	UniverseVersionID string                `json:"universeVersionId"`
	Cash              CashView              `json:"cash"`
	InitialCash       string                `json:"initialCash"`
	Version           int                   `json:"version"`
	AutomationEnabled bool                  `json:"automationEnabled"`
	PauseReason       string                `json:"pauseReason"`
	Policy            domain.RiskPolicy     `json:"policy"`
	PendingConfig     *domain.AccountConfig `json:"pendingConfig"`
	CreatedAt         time.Time             `json:"createdAt"`
}

func ViewAccount(a domain.Account) AccountView {
	return AccountView{DatasetVersion: a.DatasetVersion, ID: a.ID, Name: a.Name, Mode: a.Mode, StrategyVersionID: a.StrategyVersionID, UniverseVersionID: a.UniverseVersionID, Cash: CashView{a.Balances.Available.String(), a.Balances.Reserved.String(), a.Balances.Unsettled.String(), a.Balances.Dividends.String()}, InitialCash: a.InitialCash.String(), Version: a.Version, AutomationEnabled: a.AutomationEnabled, PauseReason: a.PauseReason, Policy: a.Policy, PendingConfig: a.PendingConfig, CreatedAt: a.CreatedAt}
}

type VersionView struct {
	ID            string                     `json:"id"`
	ParentID      string                     `json:"parentId"`
	Name          string                     `json:"name"`
	Mode          string                     `json:"mode"`
	CreatedAt     time.Time                  `json:"createdAt"`
	EffectiveAt   time.Time                  `json:"effectiveAt"`
	InstrumentIDs []string                   `json:"instrumentIds"`
	Parameters    *domain.StrategyParameters `json:"parameters"`
}

package dto

import (
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type SyncRequest struct {
	Mutation
	RunID          string    `json:"runId"`
	Mode           string    `json:"mode"`
	DatasetVersion string    `json:"datasetVersion"`
	InstrumentIDs  []string  `json:"instrumentIds"`
	From           time.Time `json:"from"`
	To             time.Time `json:"to"`
}
type SyncStatus struct {
	RunID          string    `json:"runId"`
	State          string    `json:"state"`
	Mode           string    `json:"mode"`
	DatasetVersion string    `json:"datasetVersion"`
	AsOf           time.Time `json:"asOf"`
	ErrorCode      string    `json:"errorCode,omitempty"`
}
type MarketBatch struct {
	Mode, Feed, Source string
	Instruments        []domain.Instrument
	Bars               []domain.Bar
	Calendar           domain.Calendar
	Actions            []domain.CorporateAction
	AsOf               time.Time
	QualityFlags       []string
}

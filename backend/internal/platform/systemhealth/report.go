package systemhealth

import "time"

type Status string

const (
	StatusHealthy     Status = "healthy"
	StatusDegraded    Status = "degraded"
	StatusUnavailable Status = "unavailable"
)

type Component struct {
	Status    Status    `json:"status"`
	CheckedAt time.Time `json:"checkedAt"`
	Detail    string    `json:"detail,omitempty"`
}

type Report struct {
	Status        Status               `json:"status"`
	CheckedAt     time.Time            `json:"checkedAt"`
	CorrelationID string               `json:"correlationId"`
	Components    map[string]Component `json:"components"`
}

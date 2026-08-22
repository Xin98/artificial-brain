package dto

import "time"

// Decision is one import decision line: which source record, the outcome
// (new|skipped|conflict|invalid), and why.
type Decision struct {
	Kind           string `json:"kind"`
	SourceRecordID string `json:"sourceRecordId"`
	Outcome        string `json:"outcome"`
	Reason         string `json:"reason"`
}

// Preview summarizes a bundle's import decisions before the user confirms.
// Truncated reports that Details was capped for display.
type Preview struct {
	New       int        `json:"new"`
	Skipped   int        `json:"skipped"`
	Conflicts int        `json:"conflicts"`
	Invalid   int        `json:"invalid"`
	Details   []Decision `json:"details"`
	Truncated bool       `json:"truncated"`
}

// ImportReport is a committed import's final report; it mirrors Preview and
// records when the commit happened.
type ImportReport struct {
	New         int        `json:"new"`
	Skipped     int        `json:"skipped"`
	Conflicts   int        `json:"conflicts"`
	Invalid     int        `json:"invalid"`
	Details     []Decision `json:"details"`
	Truncated   bool       `json:"truncated"`
	CommittedAt time.Time  `json:"committedAt"`
}

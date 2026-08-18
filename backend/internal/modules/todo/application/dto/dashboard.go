package dto

import "time"

// DashboardSummary is the deterministic dashboard payload. The reminder
// retry/fail counters stay zero until ITER-0003 delivers reminders (D7).
type DashboardSummary struct {
	PendingTotal       int       `json:"pendingTotal"`
	DueToday           int       `json:"dueToday"`
	Overdue            int       `json:"overdue"`
	NoDue              int       `json:"noDue"`
	CompletedLast7Days int       `json:"completedLast7Days"`
	ReminderRetrying   int       `json:"reminderRetrying"`
	ReminderFailed     int       `json:"reminderFailed"`
	CheckedAt          time.Time `json:"checkedAt"`
}

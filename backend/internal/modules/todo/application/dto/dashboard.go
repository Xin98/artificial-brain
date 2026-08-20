package dto

import "time"

// DashboardSummary is the deterministic dashboard payload. The reminder
// counters mirror the workspace's reminder delivery counts and stay zero
// when reminders are not wired.
type DashboardSummary struct {
	PendingTotal       int       `json:"pendingTotal"`
	DueToday           int       `json:"dueToday"`
	Overdue            int       `json:"overdue"`
	NoDue              int       `json:"noDue"`
	CompletedLast7Days int       `json:"completedLast7Days"`
	ReminderSucceeded  int       `json:"reminderSucceeded"`
	ReminderRetrying   int       `json:"reminderRetrying"`
	ReminderFailed     int       `json:"reminderFailed"`
	ReminderSuppressed int       `json:"reminderSuppressed"`
	CheckedAt          time.Time `json:"checkedAt"`
}

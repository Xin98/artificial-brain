package dto

import "time"

// PlanRequest asks the reminder seam to plan a delivery for a todo revision.
type PlanRequest struct {
	WorkspaceID         string
	TodoID              string
	TodoReminderVersion int
	ScheduledAtUTC      time.Time
	Channels            []string
}

// RevokeRequest asks the reminder seam to revoke every planned plan for a
// todo up to and including a reminder version.
type RevokeRequest struct {
	WorkspaceID         string
	TodoID              string
	UpToReminderVersion int
}

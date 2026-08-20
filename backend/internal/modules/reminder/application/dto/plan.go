package dto

import "time"

// PlanRequest asks the reminder seam to plan a delivery for a todo revision.
// OwnerUserID and Title snapshot the todo's owner and title at plan time so
// every delivery row stays workspace+user scoped and display-ready without
// re-reading the todo.
type PlanRequest struct {
	WorkspaceID         string
	TodoID              string
	OwnerUserID         string
	Title               string
	TodoReminderVersion int
	ScheduledAtUTC      time.Time
	Channels            []string
}

// RevokeRequest asks the reminder seam to revoke every planned plan for a
// todo up to and including a reminder version. Reason carries the caller's
// suppression reason for the still-scheduled deliveries ("todo_completed",
// "todo_deleted", or "version_stale"); the handler validates it against the
// domain's known reasons before touching any store.
type RevokeRequest struct {
	WorkspaceID         string
	TodoID              string
	UpToReminderVersion int
	Reason              string
}

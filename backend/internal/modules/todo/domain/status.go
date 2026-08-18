package domain

// Status is the lifecycle state of a todo. Deleted is terminal and hides the
// todo from regular queries; overdue is derived, never stored.
type Status string

const (
	StatusPending   Status = "pending"
	StatusCompleted Status = "completed"
	StatusDeleted   Status = "deleted"
)

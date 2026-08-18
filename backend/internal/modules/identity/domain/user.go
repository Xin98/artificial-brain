package domain

import "time"

// User is a person who logs in with a phone number and owns one personal
// workspace.
type User struct {
	ID          string
	WorkspaceID string
	Phone       string
	CreatedAt   time.Time
}

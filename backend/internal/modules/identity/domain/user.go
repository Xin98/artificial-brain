package domain

import "time"

// User is a person who logs in with a phone number and/or an email address
// and owns one personal workspace. Absent identifiers are empty strings.
type User struct {
	ID          string
	WorkspaceID string
	Phone       string
	Email       string
	CreatedAt   time.Time
}

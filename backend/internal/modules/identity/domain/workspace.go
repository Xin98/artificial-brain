package domain

import "time"

// PersonalWorkspace is the isolated space holding one user's work data.
type PersonalWorkspace struct {
	ID        string
	CreatedAt time.Time
}

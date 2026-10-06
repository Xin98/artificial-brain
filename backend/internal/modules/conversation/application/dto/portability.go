package dto

import "time"

// History shapes are the public application seam for passive transcript
// portability. They contain no pending confirmation or executable proposal.
type HistorySession struct {
	ID        string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type HistoryMessage struct {
	ID             string
	SessionID      *string
	Role           string
	Body           string
	ResolvedIntent *string
	Order          int64
	CreatedAt      time.Time
}

package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// SessionRecord and MessageRecord preserve history; neither carries executable
// confirmations or credentials. Order is the source insertion order.
type SessionRecord struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type MessageRecord struct {
	ID             string    `json:"id"`
	SessionID      *string   `json:"sessionId,omitempty"`
	Role           string    `json:"role"`
	Body           string    `json:"body"`
	ResolvedIntent *string   `json:"resolvedIntent,omitempty"`
	Order          int64     `json:"order"`
	CreatedAt      time.Time `json:"createdAt"`
}

func ValidateSessionRecord(r SessionRecord) error {
	if r.ID == "" || strings.TrimSpace(r.Title) == "" || utf8.RuneCountInString(r.Title) > 50 || r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		return recordError(r.ID, "session: id, title (1..50 characters), and timestamps required")
	}
	return nil
}

func ValidateMessageRecord(r MessageRecord) error {
	if r.ID == "" || (r.Role != "user" && r.Role != "assistant") || r.Order < 1 || r.CreatedAt.IsZero() || (r.SessionID != nil && *r.SessionID == "") {
		return recordError(r.ID, "message: id, known role, positive order, and timestamp required")
	}
	return nil
}

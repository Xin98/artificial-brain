package dto

import "time"

// VerifyLoginChallengeResult is returned when a login code is accepted.
type VerifyLoginChallengeResult struct {
	Token     string
	Principal Principal
	ExpiresAt time.Time
}

// SessionView describes the current authenticated session.
type SessionView struct {
	UserID      string `json:"userId"`
	WorkspaceID string `json:"workspaceId"`
	SessionID   string `json:"sessionId"`
}

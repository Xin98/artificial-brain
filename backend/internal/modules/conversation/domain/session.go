package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Session title bounds. MaxSessionTitleRunes is the user-editable limit;
// DefaultSessionTitleRunes is how much of the first message an auto-created
// session borrows for its title.
const (
	MaxSessionTitleRunes     = 50
	DefaultSessionTitleRunes = 30
)

// DefaultSessionTitleFallback titles auto-created sessions whose first
// message has no visible content after trimming.
const DefaultSessionTitleFallback = "新会话"

// Session is one conversation thread owned by a single user inside a single
// workspace. Every turn persists its transcript rows against a session, and
// the session's UpdatedAt orders the sidebar listing.
type Session struct {
	ID          string
	WorkspaceID string
	UserID      string
	Title       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewSession validates the ownership and title invariants: non-empty
// identifiers and a title of 1..MaxSessionTitleRunes runes after trimming.
func NewSession(id, workspaceID, userID, title string, now time.Time) (Session, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(userID) == "" {
		return Session{}, fmt.Errorf("%w: identifiers are required", ErrInvalidSession)
	}
	trimmed := strings.TrimSpace(title)
	if runes := utf8.RuneCountInString(trimmed); runes < 1 || runes > MaxSessionTitleRunes {
		return Session{}, fmt.Errorf("%w: title must be 1..%d characters", ErrSessionTitleInvalid, MaxSessionTitleRunes)
	}
	return Session{
		ID:          id,
		WorkspaceID: workspaceID,
		UserID:      userID,
		Title:       trimmed,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// Renamed returns a copy of the session carrying the validated new title
// and an UpdatedAt of now. The stored row is updated by the store under the
// caller's workspace+user scope.
func (s Session) Renamed(title string, now time.Time) (Session, error) {
	trimmed := strings.TrimSpace(title)
	if runes := utf8.RuneCountInString(trimmed); runes < 1 || runes > MaxSessionTitleRunes {
		return Session{}, fmt.Errorf("%w: title must be 1..%d characters", ErrSessionTitleInvalid, MaxSessionTitleRunes)
	}
	s.Title = trimmed
	s.UpdatedAt = now
	return s, nil
}

// DefaultSessionTitle derives an auto-created session's title from the first
// message: the trimmed text truncated to DefaultSessionTitleRunes runes, or
// the fallback when nothing visible remains.
func DefaultSessionTitle(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return DefaultSessionTitleFallback
	}
	runes := []rune(trimmed)
	if len(runes) > DefaultSessionTitleRunes {
		return string(runes[:DefaultSessionTitleRunes])
	}
	return trimmed
}

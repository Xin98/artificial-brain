package ports

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
)

// Transcript row roles. RoleUser marks rows written for user turns;
// RoleAssistant marks the paired assistant rows (model reply or
// deterministic server-built summary).
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// MessageLog is one transcript row for a conversation turn. SessionID is
// nil only for legacy audit rows written before sessions existed.
type MessageLog struct {
	WorkspaceID    string
	UserID         string
	Role           string
	Body           string
	SessionID      *string
	ResolvedIntent *string
	CreatedAt      time.Time
}

// MessageLogEntry is one stored transcript row as read back for history.
// ID is the decimal bigserial insertion id, ordering the transcript.
type MessageLogEntry struct {
	ID             string
	SessionID      string
	Role           string
	Body           string
	ResolvedIntent *string
	CreatedAt      time.Time
}

// ConfirmationStore persists confirmation requests. Implementations resolve
// their executor from context so writes join the caller's transaction.
type ConfirmationStore interface {
	Save(ctx context.Context, confirmation domain.ConfirmationRequest) error
	Get(ctx context.Context, workspaceID, userID, confirmationID string) (domain.ConfirmationRequest, error)
	// Consume atomically marks an unconsumed, unexpired confirmation as
	// consumed; otherwise it returns domain.ErrConfirmationConsumed or
	// domain.ErrConfirmationExpired.
	Consume(ctx context.Context, workspaceID, userID, confirmationID string, now time.Time) error
}

// MessageLogStore appends transcript rows and reads session history.
type MessageLogStore interface {
	Append(ctx context.Context, message MessageLog) error
	// ListBySession returns the latest limit rows of one session in
	// ascending insertion order, scoped to the caller's workspace+user.
	ListBySession(ctx context.Context, workspaceID, userID, sessionID string, limit int) ([]MessageLogEntry, error)
}

// SessionStore persists conversation sessions. Implementations resolve
// their executor from context so writes join the caller's transaction.
// Every accessor is scoped to workspace+user; a miss yields
// domain.ErrSessionNotFound.
type SessionStore interface {
	Create(ctx context.Context, session domain.Session) error
	Get(ctx context.Context, workspaceID, userID, sessionID string) (domain.Session, error)
	// List returns up to limit sessions ordered by UpdatedAt descending.
	List(ctx context.Context, workspaceID, userID string, limit int) ([]domain.Session, error)
	Rename(ctx context.Context, workspaceID, userID, sessionID, title string, now time.Time) error
	Delete(ctx context.Context, workspaceID, userID, sessionID string) error
	// Touch advances UpdatedAt so the sidebar re-orders on activity.
	Touch(ctx context.Context, workspaceID, userID, sessionID string, now time.Time) error
}

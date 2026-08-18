package ports

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
)

// RoleUser marks audit-log rows written for user turns.
const RoleUser = "user"

// MessageLog is one audit row for a conversation turn.
type MessageLog struct {
	WorkspaceID    string
	UserID         string
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

// MessageLogStore appends audit rows.
type MessageLogStore interface {
	Append(ctx context.Context, message MessageLog) error
}

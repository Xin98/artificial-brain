package ports

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

// TodoStore persists todos. Every method is scoped by workspace and owner;
// implementations resolve their executor from context so writes join the
// caller's ambient transaction.
type TodoStore interface {
	Insert(ctx context.Context, todo domain.Todo) error
	Get(ctx context.Context, workspaceID, ownerUserID, todoID string) (domain.Todo, error)
	// Update applies the todo when the stored row still carries
	// expectedVersion; a stale row yields domain.ErrConflict.
	Update(ctx context.Context, todo domain.Todo, expectedVersion int) error
	// List never returns deleted todos and caps the result at limit.
	List(ctx context.Context, workspaceID, ownerUserID string, filters dto.ListFilters, limit int) ([]domain.Todo, error)
	// Dashboard returns the todo counters; dueTodayStart/dueTodayEnd bound
	// the caller's local "today" converted to UTC.
	Dashboard(ctx context.Context, workspaceID, ownerUserID string, now, dueTodayStart, dueTodayEnd time.Time) (dto.DashboardSummary, error)
	// SearchCandidates returns pending todos whose title matches keyword,
	// capped at limit, each carrying its current Version.
	SearchCandidates(ctx context.Context, workspaceID, ownerUserID, keyword string, limit int) ([]dto.Candidate, error)
	// ListAll returns every todo for the owner regardless of status, ordered by
	// created_at with id as the tie-breaker for stable offset paging, capped
	// at limit from offset — the export seam.
	ListAll(ctx context.Context, workspaceID, ownerUserID string, offset, limit int) ([]domain.Todo, error)
}

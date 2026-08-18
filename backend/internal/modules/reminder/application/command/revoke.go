package command

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
)

// RevokePlansHandler revokes planned reminders up to a todo reminder version
// cutoff. Like PlanReminderHandler it owns no transaction and joins the
// caller's ambient transaction.
type RevokePlansHandler struct {
	Plans ports.PlanStore
	Now   func() time.Time
}

// Handle revokes every planned plan for the todo within the cutoff.
func (h *RevokePlansHandler) Handle(ctx context.Context, request dto.RevokeRequest) error {
	return h.Plans.RevokePlanned(ctx, request.WorkspaceID, request.TodoID, request.UpToReminderVersion, h.Now())
}

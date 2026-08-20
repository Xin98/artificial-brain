package command

import (
	"context"
	"log/slog"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

// RevokePlansHandler revokes planned reminders up to a todo reminder version
// cutoff, cancels their scheduled provider jobs best-effort, and finalizes
// every still-scheduled delivery as suppressed with the caller's reason (D9).
// Like PlanReminderHandler it owns no transaction and joins the caller's
// ambient transaction, so the suppressions commit or roll back atomically with
// the todo transition that drove the revoke. Correctness still does not depend
// on the cancels succeeding: a job that runs anyway re-reads the latest facts
// at execution time and finds the plan revoked or the delivery already
// suppressed.
type RevokePlansHandler struct {
	Plans      ports.PlanStore
	Deliveries ports.DeliveryStore
	Scheduler  ports.JobScheduler
	Log        *slog.Logger
	Now        func() time.Time
}

// Handle revokes every planned plan for the todo within the cutoff, cancels
// each planned provider job best-effort (cancel errors are logged and never
// fail the revoke), and then suppresses every delivery that has not started
// delivering. The order is deliberate: plans are revoked first, then jobs are
// cancelled while PlannedJobIDs can still see the scheduled rows, and only
// afterwards are those rows finalized as suppressed — suppression is never
// skipped because a cancel errored. Sending rows are untouched; an in-flight
// send keeps the execution-time re-read as its correctness boundary. Store
// errors propagate so the caller's transaction rolls back.
func (h *RevokePlansHandler) Handle(ctx context.Context, request dto.RevokeRequest) error {
	reason, err := domain.NewSuppressionReason(request.Reason)
	if err != nil {
		return err
	}
	if err := h.Plans.RevokePlanned(ctx, request.WorkspaceID, request.TodoID, request.UpToReminderVersion, h.Now()); err != nil {
		return err
	}
	jobIDs, err := h.Deliveries.PlannedJobIDs(ctx, request.WorkspaceID, request.TodoID, request.UpToReminderVersion)
	if err != nil {
		return err
	}
	for _, jobID := range jobIDs {
		if err := h.Scheduler.Cancel(ctx, jobID); err != nil {
			h.Log.Warn("reminder: cancel planned delivery job failed",
				slog.String("workspaceId", request.WorkspaceID),
				slog.String("todoId", request.TodoID),
				slog.Int64("jobId", jobID),
				slog.String("error", err.Error()),
			)
		}
	}
	scheduled, err := h.Deliveries.ScheduledForSuppression(ctx, request.WorkspaceID, request.TodoID, request.UpToReminderVersion)
	if err != nil {
		return err
	}
	for index := range scheduled {
		if err := scheduled[index].MarkSuppressed(reason, h.Now()); err != nil {
			return err
		}
		if err := h.Deliveries.Update(ctx, scheduled[index]); err != nil {
			return err
		}
	}
	return nil
}

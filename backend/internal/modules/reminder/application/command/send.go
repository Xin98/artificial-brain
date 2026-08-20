package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

// Channel values are pinned by domain.NewDelivery; they are repeated here as
// dispatch keys instead of importing a shared constant package.
const (
	channelEmail = "email"
	channelSMS   = "sms"
)

// Todo status strings carried by dto.TodoView. They mirror the todo module's
// status values so the reminder application layer never imports the todo
// module; the cmd-local TodoReader shim copies them across.
const (
	todoStatusDeleted   = "deleted"
	todoStatusCompleted = "completed"
)

// fallbackTitle snapshots a title onto defensively created delivery rows
// whose todo could not be re-read (revoked plan, missing todo) so the
// dashboard and audit trail can still render the row.
const fallbackTitle = "未命名提醒"

// Dead-letter reasons recorded as the delivery's LastErrorCode.
const (
	reasonPermanentFailure = "permanent_failure"
	reasonRetryExhausted   = "retry_exhausted"
)

// SendRequest asks the handler to deliver one channel of one planned
// reminder. WorkspaceID and OwnerUserID travel in the River job args so every
// execution-time read stays workspace+user scoped. FinalAttempt marks the
// queue's last retry: a transient failure on it dead-letters the delivery
// instead of returning an error.
type SendRequest struct {
	WorkspaceID  string
	OwnerUserID  string
	PlanID       string
	Channel      string
	FinalAttempt bool
}

// SendReminderHandler delivers one planned reminder channel. It re-reads the
// plan, the todo, and the channel endpoint at execution time and suppresses
// when the world changed since planning; it is idempotent on the delivery's
// business idempotency key, so retried and duplicate executions never send
// twice.
type SendReminderHandler struct {
	Plans      ports.PlanStore
	Deliveries ports.DeliveryStore
	Todos      ports.TodoReader
	Channels   ports.ChannelResolver
	Email      ports.EmailNotifier
	Sms        ports.SmsNotifier
	Log        *slog.Logger
	NewID      func() string
	Now        func() time.Time
}

// Handle delivers the reminder described by request, or suppresses it when an
// execution-time re-read shows it is no longer wanted.
func (h *SendReminderHandler) Handle(ctx context.Context, request SendRequest) error {
	plan, err := h.Plans.Get(ctx, request.WorkspaceID, request.PlanID)
	if err != nil {
		if errors.Is(err, domain.ErrPlanNotFound) {
			h.Log.Info("reminder send skipped: plan not found",
				slog.String("workspaceId", request.WorkspaceID),
				slog.String("planId", request.PlanID),
			)
			return nil
		}
		return err
	}
	key := domain.IdempotencyKeyFor(plan.WorkspaceID, plan.TodoID, plan.TodoReminderVersion, request.Channel)
	if plan.IsRevoked() {
		return h.suppress(ctx, plan, key, request, domain.ReasonPlanRevoked, fallbackTitle)
	}
	todo, err := h.Todos.Get(ctx, request.WorkspaceID, request.OwnerUserID, plan.TodoID)
	if err != nil {
		if errors.Is(err, ports.ErrTodoNotFound) {
			return h.suppress(ctx, plan, key, request, domain.ReasonTodoDeleted, fallbackTitle)
		}
		return err
	}
	switch {
	case todo.Status == todoStatusDeleted:
		return h.suppress(ctx, plan, key, request, domain.ReasonTodoDeleted, todo.Title)
	case todo.Status == todoStatusCompleted:
		return h.suppress(ctx, plan, key, request, domain.ReasonTodoCompleted, todo.Title)
	case todo.ReminderVersion != plan.TodoReminderVersion:
		return h.suppress(ctx, plan, key, request, domain.ReasonVersionStale, todo.Title)
	}
	delivery, created, err := h.loadOrCreateDelivery(ctx, plan, key, request, todo.Title)
	if err != nil {
		return err
	}
	if delivery.IsFinal() {
		return nil
	}
	if created {
		if err := h.Deliveries.Save(ctx, delivery); err != nil {
			if !errors.Is(err, domain.ErrDeliveryExists) {
				return err
			}
			// A concurrent execution inserted the row first; continue from it.
			if delivery, err = h.Deliveries.ByIdempotencyKey(ctx, plan.WorkspaceID, key); err != nil {
				return err
			}
			if delivery.IsFinal() {
				return nil
			}
		}
	}
	if err := delivery.MarkSending(h.Now()); err != nil {
		return err
	}
	if err := h.Deliveries.Update(ctx, delivery); err != nil {
		return err
	}
	endpoint, err := h.Channels.Resolve(ctx, request.WorkspaceID, request.OwnerUserID, request.Channel)
	if err != nil {
		return err
	}
	if !endpoint.Usable {
		return h.suppress(ctx, plan, key, request, domain.ReasonChannelUnavailable, delivery.TodoTitleSnapshot)
	}
	result, err := h.notify(ctx, request.Channel, dto.ReminderMessage{
		To:             endpoint.Address,
		TodoID:         delivery.TodoID,
		Title:          delivery.TodoTitleSnapshot,
		ScheduledAtUTC: delivery.ScheduledAt,
	})
	if err != nil {
		if errors.Is(err, ports.ErrPermanent) {
			code := reasonPermanentFailure
			var permanent *ports.PermanentError
			if errors.As(err, &permanent) && permanent.Code != "" {
				code = permanent.Code
			}
			return h.deadLetter(ctx, delivery, code, err)
		}
		if request.FinalAttempt {
			return h.deadLetter(ctx, delivery, reasonRetryExhausted, err)
		}
		return err
	}
	if err := delivery.MarkSucceeded(result.ProviderMessageID, h.Now()); err != nil {
		return err
	}
	return h.Deliveries.Update(ctx, delivery)
}

// loadOrCreateDelivery resolves the delivery by its business idempotency key;
// a missing row (which must not happen — deliveries are planned atomically
// with the plan) is defensively rebuilt from the plan and the re-read todo so
// the run still lands on an auditable row.
func (h *SendReminderHandler) loadOrCreateDelivery(ctx context.Context, plan domain.ReminderPlan, key string, request SendRequest, title string) (domain.ReminderDelivery, bool, error) {
	delivery, err := h.Deliveries.ByIdempotencyKey(ctx, plan.WorkspaceID, key)
	if err == nil {
		return delivery, false, nil
	}
	if !errors.Is(err, domain.ErrDeliveryNotFound) {
		return domain.ReminderDelivery{}, false, err
	}
	created, err := domain.NewDelivery(h.NewID(), plan.WorkspaceID, request.OwnerUserID, plan.TodoID,
		plan.TodoReminderVersion, plan.ID, request.Channel, title, plan.ScheduledAtUTC, h.Now())
	if err != nil {
		return domain.ReminderDelivery{}, false, err
	}
	h.Log.Warn("reminder delivery row missing; defensively created",
		slog.String("workspaceId", plan.WorkspaceID),
		slog.String("todoId", plan.TodoID),
		slog.String("channel", request.Channel),
		slog.String("idempotencyKey", key),
	)
	return created, true, nil
}

// suppress finalizes the idempotency-keyed delivery with reason. A missing
// row is still created so the dashboard and audit trail show the reason.
func (h *SendReminderHandler) suppress(ctx context.Context, plan domain.ReminderPlan, key string, request SendRequest, reason domain.SuppressionReason, title string) error {
	delivery, created, err := h.loadOrCreateDelivery(ctx, plan, key, request, title)
	if err != nil {
		return err
	}
	if delivery.IsFinal() {
		return nil
	}
	if created {
		if err := delivery.MarkSuppressed(reason, h.Now()); err != nil {
			return err
		}
		if err := h.Deliveries.Save(ctx, delivery); err != nil {
			if !errors.Is(err, domain.ErrDeliveryExists) {
				return err
			}
			// A concurrent execution inserted the row first; suppress that one.
			if delivery, err = h.Deliveries.ByIdempotencyKey(ctx, plan.WorkspaceID, key); err != nil {
				return err
			}
			if delivery.IsFinal() {
				return nil
			}
		} else {
			return nil
		}
	}
	if err := delivery.MarkSuppressed(reason, h.Now()); err != nil {
		return err
	}
	return h.Deliveries.Update(ctx, delivery)
}

// notify dispatches the message to the notifier matching the channel.
func (h *SendReminderHandler) notify(ctx context.Context, channel string, message dto.ReminderMessage) (dto.SendResult, error) {
	switch channel {
	case channelEmail:
		return h.Email.Send(ctx, message)
	case channelSMS:
		return h.Sms.Send(ctx, message)
	default:
		return dto.SendResult{}, fmt.Errorf("reminder: unsupported channel %q", channel)
	}
}

// deadLetter finalizes the delivery as failed with code and logs a structured
// dead-letter event; it never returns the provider error.
func (h *SendReminderHandler) deadLetter(ctx context.Context, delivery domain.ReminderDelivery, code string, cause error) error {
	if err := delivery.MarkFailed(code, h.Now()); err != nil {
		return err
	}
	if err := h.Deliveries.Update(ctx, delivery); err != nil {
		return err
	}
	h.Log.Error("reminder delivery dead-lettered",
		slog.String("workspaceId", delivery.WorkspaceID),
		slog.String("todoId", delivery.TodoID),
		slog.String("channel", delivery.Channel),
		slog.String("reason", code),
		slog.String("error", cause.Error()),
	)
	return nil
}

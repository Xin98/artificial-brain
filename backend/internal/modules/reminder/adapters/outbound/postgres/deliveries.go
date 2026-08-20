package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// deliveryColumns is the full column list of reminder.reminder_deliveries in
// scan order; Save inserts and every read selects exactly this shape.
const deliveryColumns = `id, workspace_id, owner_user_id, todo_id, todo_reminder_version,
	plan_id, channel, todo_title_snapshot, idempotency_key, state, suppression_reason,
	attempt_count, provider_job_id, provider_message_id, last_error_code,
	scheduled_at, created_at, submitted_at, finalized_at,
	receipt_state, receipt_at, receipt_error_code`

// DeliveryStore persists reminder deliveries in PostgreSQL.
type DeliveryStore struct {
	pool *pgxpool.Pool
}

// NewDeliveryStore returns a DeliveryStore bound to pool.
func NewDeliveryStore(pool *pgxpool.Pool) *DeliveryStore { return &DeliveryStore{pool: pool} }

var _ ports.DeliveryStore = (*DeliveryStore)(nil)

// Save inserts a delivery, mapping any unique violation (the idempotency key
// or the todo/version/channel fallback) to domain.ErrDeliveryExists so
// replanning stays idempotent.
func (s *DeliveryStore) Save(ctx context.Context, delivery domain.ReminderDelivery) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		insert into reminder.reminder_deliveries (`+deliveryColumns+`)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
			$16, $17, $18, $19, $20, $21, $22)
	`, deliveryArgs(delivery)...)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrDeliveryExists
	}
	return err
}

// Update replaces a delivery's mutable fields, keyed by (workspace_id, id).
// Identity and planning fields (todo, plan, channel, idempotency key, title
// snapshot, created_at) are immutable once planned; provider_job_id is owned
// exclusively by SetProviderJobID and scheduled_at is fixed at planning, so
// neither can be clobbered by an Update carrying a stale in-memory struct. A
// missing row maps to domain.ErrDeliveryNotFound, matching the read paths.
func (s *DeliveryStore) Update(ctx context.Context, delivery domain.ReminderDelivery) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	tag, err := exec.Exec(ctx, `
		update reminder.reminder_deliveries
		set state = $3,
			suppression_reason = $4,
			attempt_count = $5,
			provider_message_id = $6,
			last_error_code = $7,
			submitted_at = $8,
			finalized_at = $9,
			receipt_state = $10,
			receipt_at = $11,
			receipt_error_code = $12
		where workspace_id = $1 and id = $2
	`, delivery.WorkspaceID, delivery.ID, string(delivery.State),
		suppressionReasonArg(delivery.SuppressionReason), delivery.AttemptCount,
		delivery.ProviderMessageID, delivery.LastErrorCode,
		delivery.SubmittedAt, delivery.FinalizedAt,
		receiptStateArg(delivery.ReceiptState), delivery.ReceiptAt, delivery.ReceiptErrorCode)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrDeliveryNotFound
	}
	return nil
}

// ByIdempotencyKey loads one delivery scoped by workspace; a missing row maps
// to domain.ErrDeliveryNotFound.
func (s *DeliveryStore) ByIdempotencyKey(ctx context.Context, workspaceID, key string) (domain.ReminderDelivery, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	row := exec.QueryRow(ctx, `
		select `+deliveryColumns+`
		from reminder.reminder_deliveries
		where workspace_id = $1 and idempotency_key = $2
	`, workspaceID, key)
	return scanDelivery(row)
}

// ByProviderMessageID loads one delivery by its provider message ID. This is
// the documented D6 exception to workspace scoping: provider webhooks carry
// no workspace, so the lookup is provider-keyed. A missing row maps to
// domain.ErrDeliveryNotFound.
func (s *DeliveryStore) ByProviderMessageID(ctx context.Context, providerMessageID string) (domain.ReminderDelivery, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	row := exec.QueryRow(ctx, `
		select `+deliveryColumns+`
		from reminder.reminder_deliveries
		where provider_message_id = $1
	`, providerMessageID)
	return scanDelivery(row)
}

// SetProviderJobID writes the scheduler-assigned job ID back onto the
// delivery row.
func (s *DeliveryStore) SetProviderJobID(ctx context.Context, workspaceID, deliveryID string, jobID int64) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		update reminder.reminder_deliveries
		set provider_job_id = $3
		where workspace_id = $1 and id = $2
	`, workspaceID, deliveryID, jobID)
	return err
}

// PlannedJobIDs returns the provider job IDs of every non-final delivery for
// the todo at or below the reminder version cutoff, ordered by job ID.
// Final deliveries (already succeeded, failed, or suppressed) need no
// cancellation, so their jobs are excluded.
func (s *DeliveryStore) PlannedJobIDs(ctx context.Context, workspaceID, todoID string, upToReminderVersion int) ([]int64, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	rows, err := exec.Query(ctx, `
		select provider_job_id
		from reminder.reminder_deliveries
		where workspace_id = $1 and todo_id = $2
		  and todo_reminder_version <= $3
		  and provider_job_id is not null
		  and state in ('scheduled', 'sending')
		order by provider_job_id
	`, workspaceID, todoID, upToReminderVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobIDs := []int64{}
	for rows.Next() {
		var jobID int64
		if err := rows.Scan(&jobID); err != nil {
			return nil, err
		}
		jobIDs = append(jobIDs, jobID)
	}
	return jobIDs, rows.Err()
}

// ScheduledForSuppression returns every delivery for the todo at or below the
// reminder version cutoff that is still scheduled, ordered by id. Sending rows
// are excluded — an in-flight send keeps the execution-time re-read as its
// correctness boundary — and final rows never transition again. Reads resolve
// the executor from context, so the revoke handler's suppressions join the
// caller's ambient transaction.
func (s *DeliveryStore) ScheduledForSuppression(ctx context.Context, workspaceID, todoID string, upToReminderVersion int) ([]domain.ReminderDelivery, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	rows, err := exec.Query(ctx, `
		select `+deliveryColumns+`
		from reminder.reminder_deliveries
		where workspace_id = $1 and todo_id = $2
		  and todo_reminder_version <= $3
		  and state = 'scheduled'
		order by id
	`, workspaceID, todoID, upToReminderVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	deliveries := []domain.ReminderDelivery{}
	for rows.Next() {
		delivery, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}

// Stats counts deliveries per lifecycle bucket for the workspace. Sending is
// split into first-attempt sending and retrying (attempt_count > 0).
func (s *DeliveryStore) Stats(ctx context.Context, workspaceID string) (dto.DeliveryCounts, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	var counts dto.DeliveryCounts
	err := exec.QueryRow(ctx, `
		select
			count(*) filter (where state = 'scheduled'),
			count(*) filter (where state = 'sending' and attempt_count = 0),
			count(*) filter (where state = 'sending' and attempt_count > 0),
			count(*) filter (where state = 'succeeded'),
			count(*) filter (where state = 'failed'),
			count(*) filter (where state = 'suppressed')
		from reminder.reminder_deliveries
		where workspace_id = $1
	`, workspaceID).Scan(&counts.Scheduled, &counts.Sending, &counts.Retrying,
		&counts.Succeeded, &counts.Failed, &counts.Suppressed)
	if err != nil {
		return dto.DeliveryCounts{}, err
	}
	return counts, nil
}

// List returns deliveries for the workspace matching the filter, newest
// first. The status filter accepts the five lifecycle states plus the
// "retrying" alias (sending with at least one retry); plain "sending" matches
// any sending row and an empty status applies no filter. Limit and offset are
// applied as given; the application layer clamps them.
func (s *DeliveryStore) List(ctx context.Context, workspaceID string, filter dto.DeliveryFilter) ([]domain.ReminderDelivery, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	rows, err := exec.Query(ctx, `
		select `+deliveryColumns+`
		from reminder.reminder_deliveries
		where workspace_id = $1
		  and ($2 = ''
		    or state = $2
		    or ($2 = 'retrying' and state = 'sending' and attempt_count > 0))
		order by created_at desc, id desc
		limit $3 offset $4
	`, workspaceID, filter.Status, filter.Limit, filter.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deliveries []domain.ReminderDelivery
	for rows.Next() {
		delivery, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// deliveryArgs returns the 22 insert arguments for Save in column order.
func deliveryArgs(delivery domain.ReminderDelivery) []any {
	return []any{
		delivery.ID, delivery.WorkspaceID, delivery.OwnerUserID, delivery.TodoID,
		delivery.TodoReminderVersion, delivery.PlanID, delivery.Channel,
		delivery.TodoTitleSnapshot, delivery.IdempotencyKey, string(delivery.State),
		suppressionReasonArg(delivery.SuppressionReason), delivery.AttemptCount,
		delivery.ProviderJobID, delivery.ProviderMessageID, delivery.LastErrorCode,
		delivery.ScheduledAt, delivery.CreatedAt, delivery.SubmittedAt,
		delivery.FinalizedAt, receiptStateArg(delivery.ReceiptState),
		delivery.ReceiptAt, delivery.ReceiptErrorCode,
	}
}

func suppressionReasonArg(reason *domain.SuppressionReason) *string {
	if reason == nil {
		return nil
	}
	value := string(*reason)
	return &value
}

func receiptStateArg(state *domain.ReceiptState) *string {
	if state == nil {
		return nil
	}
	value := string(*state)
	return &value
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

// scanDelivery reads one delivery row in deliveryColumns order; a missing row
// maps to domain.ErrDeliveryNotFound.
func scanDelivery(scanner rowScanner) (domain.ReminderDelivery, error) {
	var delivery domain.ReminderDelivery
	var state, suppressionReason, receiptState *string
	err := scanner.Scan(&delivery.ID, &delivery.WorkspaceID, &delivery.OwnerUserID,
		&delivery.TodoID, &delivery.TodoReminderVersion, &delivery.PlanID,
		&delivery.Channel, &delivery.TodoTitleSnapshot, &delivery.IdempotencyKey,
		&state, &suppressionReason, &delivery.AttemptCount, &delivery.ProviderJobID,
		&delivery.ProviderMessageID, &delivery.LastErrorCode, &delivery.ScheduledAt,
		&delivery.CreatedAt, &delivery.SubmittedAt, &delivery.FinalizedAt,
		&receiptState, &delivery.ReceiptAt, &delivery.ReceiptErrorCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ReminderDelivery{}, domain.ErrDeliveryNotFound
	}
	if err != nil {
		return domain.ReminderDelivery{}, err
	}
	// pgx scans timestamptz in the local location; the domain works in UTC,
	// so normalize every scanned instant before handing it back.
	delivery.ScheduledAt = delivery.ScheduledAt.UTC()
	delivery.CreatedAt = delivery.CreatedAt.UTC()
	delivery.SubmittedAt = utcPtr(delivery.SubmittedAt)
	delivery.FinalizedAt = utcPtr(delivery.FinalizedAt)
	delivery.ReceiptAt = utcPtr(delivery.ReceiptAt)
	if state != nil {
		delivery.State = domain.DeliveryState(*state)
	}
	if suppressionReason != nil {
		reason := domain.SuppressionReason(*suppressionReason)
		delivery.SuppressionReason = &reason
	}
	if receiptState != nil {
		receipt := domain.ReceiptState(*receiptState)
		delivery.ReceiptState = &receipt
	}
	return delivery, nil
}

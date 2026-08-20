package postgres

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

// cleanRiverJobs removes any reminder-queue rows so each ops test starts from
// a known backlog.
func cleanRiverJobs(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		delete from river_job where queue in ('reminder_email', 'reminder_sms')
	`); err != nil {
		t.Fatalf("clean river jobs error = %v", err)
	}
}

// seedRiverJob inserts one river_job row directly. finalizedAt must be nil for
// non-final states (the table's check constraint enforces this); args is NOT
// NULL after migration 006's backfill.
func seedRiverJob(t *testing.T, pool *pgxpool.Pool, queue, state string, scheduledAt time.Time, finalizedAt *time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		insert into river_job (kind, max_attempts, queue, state, scheduled_at, finalized_at, args)
		values ('reminder.send', 5, $1, $2, $3, $4, '{}'::jsonb)
	`, queue, state, scheduledAt, finalizedAt); err != nil {
		t.Fatalf("seed river job error = %v", err)
	}
}

// seedOpsDelivery saves one delivery for a fresh todo across two workspaces,
// mutating it before persisting so any lifecycle shape can be seeded.
func seedOpsDelivery(t *testing.T, ctx context.Context, plans *PlanStore, deliveries *DeliveryStore, workspaceID string, mutate func(*domain.ReminderDelivery)) {
	t.Helper()
	todoID := randomID(t)
	plan := seedPlanFixture(t, ctx, plans, workspaceID, todoID, 1)
	delivery := newDeliveryFixture(t, workspaceID, randomID(t), todoID, 1, plan.ID, "email", testNow.Add(time.Hour))
	if mutate != nil {
		mutate(&delivery)
	}
	if err := deliveries.Save(ctx, delivery); err != nil {
		t.Fatalf("Save(ops fixture) error = %v", err)
	}
}

func TestOpsStoreSeededQueuesAndDeliveries(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewOpsStore(pool)
	plans := NewPlanStore(pool)
	deliveries := NewDeliveryStore(pool)
	cleanRiverJobs(t, pool)
	t.Cleanup(func() { cleanRiverJobs(t, pool) })

	// Backlog: two waiting email jobs (oldest 90s), one waiting sms job (5s),
	// and one completed email job that must be ignored.
	seedRiverJob(t, pool, "reminder_email", "available", testNow.Add(-90*time.Second), nil)
	seedRiverJob(t, pool, "reminder_email", "scheduled", testNow.Add(-30*time.Second), nil)
	finalizedAt := testNow.Add(-1 * time.Hour)
	seedRiverJob(t, pool, "reminder_email", "completed", testNow.Add(-2*time.Hour), &finalizedAt)
	seedRiverJob(t, pool, "reminder_sms", "retryable", testNow.Add(-5*time.Second), nil)

	// Instance-wide deliveries across two workspaces.
	workspaceA, workspaceB := randomID(t), randomID(t)
	seedOpsDelivery(t, ctx, plans, deliveries, workspaceA, func(d *domain.ReminderDelivery) {
		d.State = domain.StateSending // first attempt
	})
	seedOpsDelivery(t, ctx, plans, deliveries, workspaceA, func(d *domain.ReminderDelivery) {
		d.State = domain.StateSending
		d.AttemptCount = 3
	})
	// Succeeded inside the window with a known 250ms submission latency.
	seedOpsDelivery(t, ctx, plans, deliveries, workspaceB, func(d *domain.ReminderDelivery) {
		d.State = domain.StateSucceeded
		d.AttemptCount = 1
		d.ScheduledAt = testNow.Add(-2 * time.Hour)
		submittedAt := testNow.Add(-2 * time.Hour).Add(250 * time.Millisecond)
		d.SubmittedAt = &submittedAt
		d.FinalizedAt = &submittedAt
	})
	seedOpsDelivery(t, ctx, plans, deliveries, workspaceB, nil) // scheduled
	// Succeeded outside the 24h window: must not affect the latency percentile.
	seedOpsDelivery(t, ctx, plans, deliveries, workspaceB, func(d *domain.ReminderDelivery) {
		d.State = domain.StateSucceeded
		d.ScheduledAt = testNow.Add(-48 * time.Hour)
		submittedAt := testNow.Add(-48 * time.Hour).Add(5 * time.Second)
		d.SubmittedAt = &submittedAt
		d.FinalizedAt = &submittedAt
	})

	view, err := store.ReminderOps(ctx, testNow, 24*time.Hour)
	if err != nil {
		t.Fatalf("ReminderOps() error = %v", err)
	}
	want := dto.OpsView{
		Queues: []dto.QueueDepth{
			{Queue: "reminder_email", Depth: 2, OldestWaitSeconds: 90},
			{Queue: "reminder_sms", Depth: 1, OldestWaitSeconds: 5},
		},
		Deliveries:   dto.DeliveryCounts{Scheduled: 1, Sending: 1, Retrying: 1, Succeeded: 2},
		RetryRate:    0.4, // two of five deliveries retried
		LatencyP95Ms: 250, // single in-window succeeded delivery
		CheckedAt:    testNow,
	}
	if !reflect.DeepEqual(view, want) {
		t.Fatalf("ReminderOps() = %#v, want %#v", view, want)
	}
}

func TestOpsStoreReportsZeroRowsForEmptyQueuesAndDeliveries(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewOpsStore(pool)
	cleanRiverJobs(t, pool)
	t.Cleanup(func() { cleanRiverJobs(t, pool) })

	view, err := store.ReminderOps(ctx, testNow, 24*time.Hour)
	if err != nil {
		t.Fatalf("ReminderOps() error = %v", err)
	}
	want := dto.OpsView{
		Queues: []dto.QueueDepth{
			{Queue: "reminder_email", Depth: 0, OldestWaitSeconds: 0},
			{Queue: "reminder_sms", Depth: 0, OldestWaitSeconds: 0},
		},
		Deliveries:   dto.DeliveryCounts{},
		RetryRate:    0,
		LatencyP95Ms: 0,
		CheckedAt:    testNow,
	}
	if !reflect.DeepEqual(view, want) {
		t.Fatalf("ReminderOps() = %#v, want %#v", view, want)
	}
}

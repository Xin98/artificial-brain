package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// newDeliveryFixture builds a valid scheduled delivery for one channel of a plan.
func newDeliveryFixture(t *testing.T, workspaceID, ownerUserID, todoID string, reminderVersion int, planID, channel string, scheduledAt time.Time) domain.ReminderDelivery {
	t.Helper()
	delivery, err := domain.NewDelivery(randomID(t), workspaceID, ownerUserID, todoID,
		reminderVersion, planID, channel, "review the launch checklist", scheduledAt, testNow)
	if err != nil {
		t.Fatalf("NewDelivery() error = %v", err)
	}
	return delivery
}

// seedPlanFixture saves a fresh plan for (workspace, todo, version) and returns it.
func seedPlanFixture(t *testing.T, ctx context.Context, plans *PlanStore, workspaceID, todoID string, reminderVersion int) domain.ReminderPlan {
	t.Helper()
	plan := newPlan(t, workspaceID, todoID, reminderVersion)
	if err := plans.Save(ctx, plan); err != nil {
		t.Fatalf("Save(plan) error = %v", err)
	}
	return plan
}

func TestDeliveryStoreSaveAndRoundTrip(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewDeliveryStore(pool)
	plans := NewPlanStore(pool)
	workspaceID, ownerUserID, todoID := randomID(t), randomID(t), randomID(t)
	plan := seedPlanFixture(t, ctx, plans, workspaceID, todoID, 1)

	scheduledAt := testNow.Add(2 * time.Hour)
	delivery := newDeliveryFixture(t, workspaceID, ownerUserID, todoID, 1, plan.ID, "email", scheduledAt)
	if err := store.Save(ctx, delivery); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Every persisted field round-trips, including the nullable optionals as nil.
	got, err := store.ByIdempotencyKey(ctx, workspaceID, delivery.IdempotencyKey)
	if err != nil {
		t.Fatalf("ByIdempotencyKey() error = %v", err)
	}
	if !reflect.DeepEqual(got, delivery) {
		t.Fatalf("ByIdempotencyKey() = %#v, want %#v", got, delivery)
	}

	// An unknown key maps to ErrDeliveryNotFound.
	if _, err := store.ByIdempotencyKey(ctx, workspaceID, "missing-key"); !errors.Is(err, domain.ErrDeliveryNotFound) {
		t.Fatalf("ByIdempotencyKey(unknown) error = %v, want ErrDeliveryNotFound", err)
	}
	// The same key in another workspace is not visible.
	if _, err := store.ByIdempotencyKey(ctx, randomID(t), delivery.IdempotencyKey); !errors.Is(err, domain.ErrDeliveryNotFound) {
		t.Fatalf("ByIdempotencyKey(wrong workspace) error = %v, want ErrDeliveryNotFound", err)
	}
}

func TestDeliveryStoreDuplicateIdempotencyKey(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewDeliveryStore(pool)
	plans := NewPlanStore(pool)
	workspaceID, ownerUserID, todoID := randomID(t), randomID(t), randomID(t)
	plan := seedPlanFixture(t, ctx, plans, workspaceID, todoID, 1)

	delivery := newDeliveryFixture(t, workspaceID, ownerUserID, todoID, 1, plan.ID, "sms", testNow.Add(time.Hour))
	if err := store.Save(ctx, delivery); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Same workspace:todo:version:channel means the same idempotency key.
	duplicate := newDeliveryFixture(t, workspaceID, ownerUserID, todoID, 1, plan.ID, "sms", testNow.Add(3*time.Hour))
	if err := store.Save(ctx, duplicate); !errors.Is(err, domain.ErrDeliveryExists) {
		t.Fatalf("duplicate Save() error = %v, want ErrDeliveryExists", err)
	}
}

func TestDeliveryStoreUpdateRoundTripsTransitions(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewDeliveryStore(pool)
	plans := NewPlanStore(pool)
	workspaceID, ownerUserID := randomID(t), randomID(t)

	// Success path: sending -> retry -> succeeded -> receipt.
	todoID := randomID(t)
	plan := seedPlanFixture(t, ctx, plans, workspaceID, todoID, 1)
	delivery := newDeliveryFixture(t, workspaceID, ownerUserID, todoID, 1, plan.ID, "email", testNow.Add(time.Hour))
	if err := store.Save(ctx, delivery); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	firstAttempt := testNow.Add(time.Minute)
	if err := delivery.MarkSending(firstAttempt); err != nil {
		t.Fatalf("MarkSending() error = %v", err)
	}
	if err := store.Update(ctx, delivery); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	got, err := store.ByIdempotencyKey(ctx, workspaceID, delivery.IdempotencyKey)
	if err != nil {
		t.Fatalf("ByIdempotencyKey() error = %v", err)
	}
	if !reflect.DeepEqual(got, delivery) {
		t.Fatalf("after first attempt = %#v, want %#v", got, delivery)
	}

	retryAt := testNow.Add(2 * time.Minute)
	if err := delivery.MarkSending(retryAt); err != nil {
		t.Fatalf("MarkSending(retry) error = %v", err)
	}
	if err := store.Update(ctx, delivery); err != nil {
		t.Fatalf("Update(retry) error = %v", err)
	}
	if got, err = store.ByIdempotencyKey(ctx, workspaceID, delivery.IdempotencyKey); err != nil {
		t.Fatalf("ByIdempotencyKey() error = %v", err)
	}
	if got.State != domain.StateSending || got.AttemptCount != 2 {
		t.Fatalf("retry round-trip state/attempt = %v/%d, want sending/2", got.State, got.AttemptCount)
	}

	submittedAt := testNow.Add(3 * time.Minute)
	if err := delivery.MarkSucceeded("provider-message-9", submittedAt); err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}
	if err := store.Update(ctx, delivery); err != nil {
		t.Fatalf("Update(succeeded) error = %v", err)
	}
	if got, err = store.ByIdempotencyKey(ctx, workspaceID, delivery.IdempotencyKey); err != nil {
		t.Fatalf("ByIdempotencyKey() error = %v", err)
	}
	if !reflect.DeepEqual(got, delivery) {
		t.Fatalf("after succeeded = %#v, want %#v", got, delivery)
	}

	receiptAt := testNow.Add(4 * time.Minute)
	if err := delivery.ApplyReceipt(domain.ReceiptFailed, "hard_bounce", receiptAt); err != nil {
		t.Fatalf("ApplyReceipt() error = %v", err)
	}
	if err := store.Update(ctx, delivery); err != nil {
		t.Fatalf("Update(receipt) error = %v", err)
	}
	if got, err = store.ByIdempotencyKey(ctx, workspaceID, delivery.IdempotencyKey); err != nil {
		t.Fatalf("ByIdempotencyKey() error = %v", err)
	}
	if !reflect.DeepEqual(got, delivery) {
		t.Fatalf("after receipt = %#v, want %#v", got, delivery)
	}

	// Failure path: last_error_code and finalized_at round-trip.
	failedTodo := randomID(t)
	failedPlan := seedPlanFixture(t, ctx, plans, workspaceID, failedTodo, 1)
	failed := newDeliveryFixture(t, workspaceID, ownerUserID, failedTodo, 1, failedPlan.ID, "sms", testNow.Add(time.Hour))
	if err := store.Save(ctx, failed); err != nil {
		t.Fatalf("Save(failed path) error = %v", err)
	}
	if err := failed.MarkFailed("invalid_address", testNow.Add(5*time.Minute)); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}
	if err := store.Update(ctx, failed); err != nil {
		t.Fatalf("Update(failed) error = %v", err)
	}
	if got, err = store.ByIdempotencyKey(ctx, workspaceID, failed.IdempotencyKey); err != nil {
		t.Fatalf("ByIdempotencyKey(failed) error = %v", err)
	}
	if !reflect.DeepEqual(got, failed) {
		t.Fatalf("after failed = %#v, want %#v", got, failed)
	}

	// Suppression path: suppression_reason round-trips.
	suppressedTodo := randomID(t)
	suppressedPlan := seedPlanFixture(t, ctx, plans, workspaceID, suppressedTodo, 1)
	suppressed := newDeliveryFixture(t, workspaceID, ownerUserID, suppressedTodo, 1, suppressedPlan.ID, "email", testNow.Add(time.Hour))
	if err := store.Save(ctx, suppressed); err != nil {
		t.Fatalf("Save(suppressed path) error = %v", err)
	}
	if err := suppressed.MarkSuppressed(domain.ReasonVersionStale, testNow.Add(6*time.Minute)); err != nil {
		t.Fatalf("MarkSuppressed() error = %v", err)
	}
	if err := store.Update(ctx, suppressed); err != nil {
		t.Fatalf("Update(suppressed) error = %v", err)
	}
	if got, err = store.ByIdempotencyKey(ctx, workspaceID, suppressed.IdempotencyKey); err != nil {
		t.Fatalf("ByIdempotencyKey(suppressed) error = %v", err)
	}
	if !reflect.DeepEqual(got, suppressed) {
		t.Fatalf("after suppressed = %#v, want %#v", got, suppressed)
	}
}

func TestDeliveryStoreSetProviderJobIDAndPlannedJobIDs(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewDeliveryStore(pool)
	plans := NewPlanStore(pool)
	workspaceID, ownerUserID, todoID := randomID(t), randomID(t), randomID(t)

	// v1 email: job set, scheduled -> included below the cutoff.
	planV1 := seedPlanFixture(t, ctx, plans, workspaceID, todoID, 1)
	v1Email := newDeliveryFixture(t, workspaceID, ownerUserID, todoID, 1, planV1.ID, "email", testNow.Add(time.Hour))
	if err := store.Save(ctx, v1Email); err != nil {
		t.Fatalf("Save(v1 email) error = %v", err)
	}
	if err := store.SetProviderJobID(ctx, workspaceID, v1Email.ID, 101); err != nil {
		t.Fatalf("SetProviderJobID(v1 email) error = %v", err)
	}

	// v1 sms: job set, sending (non-final) -> included. The job ID is written
	// before the state-transition Update to prove the two writes can happen in
	// any order: Update no longer touches provider_job_id.
	v1Sms := newDeliveryFixture(t, workspaceID, ownerUserID, todoID, 1, planV1.ID, "sms", testNow.Add(time.Hour))
	if err := store.Save(ctx, v1Sms); err != nil {
		t.Fatalf("Save(v1 sms) error = %v", err)
	}
	if err := store.SetProviderJobID(ctx, workspaceID, v1Sms.ID, 104); err != nil {
		t.Fatalf("SetProviderJobID(v1 sms) error = %v", err)
	}
	if err := v1Sms.MarkSending(testNow); err != nil {
		t.Fatalf("MarkSending(v1 sms) error = %v", err)
	}
	if err := store.Update(ctx, v1Sms); err != nil {
		t.Fatalf("Update(v1 sms) error = %v", err)
	}

	// v2 email: job set but succeeded (final) -> excluded.
	planV2 := seedPlanFixture(t, ctx, plans, workspaceID, todoID, 2)
	v2Email := newDeliveryFixture(t, workspaceID, ownerUserID, todoID, 2, planV2.ID, "email", testNow.Add(time.Hour))
	if err := v2Email.MarkSending(testNow); err != nil {
		t.Fatalf("MarkSending(v2 email) error = %v", err)
	}
	if err := v2Email.MarkSucceeded("provider-message-2", testNow.Add(time.Minute)); err != nil {
		t.Fatalf("MarkSucceeded(v2 email) error = %v", err)
	}
	if err := store.Save(ctx, v2Email); err != nil {
		t.Fatalf("Save(v2 email) error = %v", err)
	}
	if err := store.SetProviderJobID(ctx, workspaceID, v2Email.ID, 102); err != nil {
		t.Fatalf("SetProviderJobID(v2 email) error = %v", err)
	}

	// v2 sms: no job id -> excluded.
	v2Sms := newDeliveryFixture(t, workspaceID, ownerUserID, todoID, 2, planV2.ID, "sms", testNow.Add(time.Hour))
	if err := store.Save(ctx, v2Sms); err != nil {
		t.Fatalf("Save(v2 sms) error = %v", err)
	}

	// v3 email: job set but above the cutoff -> excluded.
	planV3 := seedPlanFixture(t, ctx, plans, workspaceID, todoID, 3)
	v3Email := newDeliveryFixture(t, workspaceID, ownerUserID, todoID, 3, planV3.ID, "email", testNow.Add(time.Hour))
	if err := store.Save(ctx, v3Email); err != nil {
		t.Fatalf("Save(v3 email) error = %v", err)
	}
	if err := store.SetProviderJobID(ctx, workspaceID, v3Email.ID, 103); err != nil {
		t.Fatalf("SetProviderJobID(v3 email) error = %v", err)
	}

	// The job ID is readable on the delivery row itself.
	reloaded, err := store.ByIdempotencyKey(ctx, workspaceID, v1Email.IdempotencyKey)
	if err != nil {
		t.Fatalf("ByIdempotencyKey(v1 email) error = %v", err)
	}
	if reloaded.ProviderJobID == nil || *reloaded.ProviderJobID != 101 {
		t.Fatalf("ProviderJobID = %v, want 101", reloaded.ProviderJobID)
	}

	// Cutoff 2: only the non-final deliveries at or below v2 with job IDs.
	ids, err := store.PlannedJobIDs(ctx, workspaceID, todoID, 2)
	if err != nil {
		t.Fatalf("PlannedJobIDs(cutoff 2) error = %v", err)
	}
	if !reflect.DeepEqual(ids, []int64{101, 104}) {
		t.Fatalf("PlannedJobIDs(cutoff 2) = %v, want [101 104]", ids)
	}

	// Cutoff 3 adds the v3 job.
	if ids, err = store.PlannedJobIDs(ctx, workspaceID, todoID, 3); err != nil {
		t.Fatalf("PlannedJobIDs(cutoff 3) error = %v", err)
	}
	if !reflect.DeepEqual(ids, []int64{101, 103, 104}) {
		t.Fatalf("PlannedJobIDs(cutoff 3) = %v, want [101 103 104]", ids)
	}

	// Another workspace sees nothing.
	if ids, err = store.PlannedJobIDs(ctx, randomID(t), todoID, 3); err != nil {
		t.Fatalf("PlannedJobIDs(other workspace) error = %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("PlannedJobIDs(other workspace) = %v, want none", ids)
	}
}

func TestDeliveryStoreUpdateCannotClobberJobIDOrSchedule(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewDeliveryStore(pool)
	plans := NewPlanStore(pool)
	workspaceID, ownerUserID, todoID := randomID(t), randomID(t), randomID(t)
	plan := seedPlanFixture(t, ctx, plans, workspaceID, todoID, 1)

	scheduledAt := testNow.Add(time.Hour)
	delivery := newDeliveryFixture(t, workspaceID, ownerUserID, todoID, 1, plan.ID, "email", scheduledAt)
	if err := store.Save(ctx, delivery); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.SetProviderJobID(ctx, workspaceID, delivery.ID, 42); err != nil {
		t.Fatalf("SetProviderJobID() error = %v", err)
	}

	// Regression: a stale struct read before the job-ID write carries no job ID
	// (and here a drifted schedule); the transition Update must not NULL the job
	// ID nor move the schedule, or the job could never be cancelled via
	// PlannedJobIDs.
	stale := delivery
	stale.ProviderJobID = nil
	stale.ScheduledAt = testNow.Add(99 * time.Hour)
	if err := stale.MarkSending(testNow.Add(time.Minute)); err != nil {
		t.Fatalf("MarkSending() error = %v", err)
	}
	if err := store.Update(ctx, stale); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := store.ByIdempotencyKey(ctx, workspaceID, delivery.IdempotencyKey)
	if err != nil {
		t.Fatalf("ByIdempotencyKey() error = %v", err)
	}
	if got.ProviderJobID == nil || *got.ProviderJobID != 42 {
		t.Fatalf("ProviderJobID = %v, want 42", got.ProviderJobID)
	}
	if !got.ScheduledAt.Equal(scheduledAt) {
		t.Fatalf("ScheduledAt = %v, want %v", got.ScheduledAt, scheduledAt)
	}
	// The mutable fields the stale struct did carry still round-trip.
	if got.State != domain.StateSending || got.AttemptCount != 1 {
		t.Fatalf("state/attempt = %v/%d, want sending/1", got.State, got.AttemptCount)
	}
}

func TestDeliveryStoreUpdateUnknownDelivery(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewDeliveryStore(pool)
	plans := NewPlanStore(pool)
	workspaceID, ownerUserID, todoID := randomID(t), randomID(t), randomID(t)
	plan := seedPlanFixture(t, ctx, plans, workspaceID, todoID, 1)

	// A delivery that was never saved: Update surfaces the missing row.
	missing := newDeliveryFixture(t, workspaceID, ownerUserID, todoID, 1, plan.ID, "email", testNow.Add(time.Hour))
	if err := store.Update(ctx, missing); !errors.Is(err, domain.ErrDeliveryNotFound) {
		t.Fatalf("Update(unknown id) error = %v, want ErrDeliveryNotFound", err)
	}

	// A saved delivery is invisible under another workspace's key.
	delivery := newDeliveryFixture(t, workspaceID, ownerUserID, todoID, 1, plan.ID, "sms", testNow.Add(time.Hour))
	if err := store.Save(ctx, delivery); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	delivery.WorkspaceID = randomID(t)
	if err := store.Update(ctx, delivery); !errors.Is(err, domain.ErrDeliveryNotFound) {
		t.Fatalf("Update(wrong workspace) error = %v, want ErrDeliveryNotFound", err)
	}
}

// seedStatsDelivery saves one delivery for a fresh todo whose state and
// attempt count are set directly on the struct before persisting, so every
// lifecycle bucket can be seeded.
func seedStatsDelivery(t *testing.T, ctx context.Context, plans *PlanStore, deliveries *DeliveryStore, workspaceID string, state domain.DeliveryState, attemptCount int) {
	t.Helper()
	todoID := randomID(t)
	plan := seedPlanFixture(t, ctx, plans, workspaceID, todoID, 1)
	delivery := newDeliveryFixture(t, workspaceID, randomID(t), todoID, 1, plan.ID, "email", testNow.Add(time.Hour))
	delivery.State = state
	delivery.AttemptCount = attemptCount
	if err := deliveries.Save(ctx, delivery); err != nil {
		t.Fatalf("Save(stats fixture) error = %v", err)
	}
}

func TestDeliveryStoreStatsBuckets(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewDeliveryStore(pool)
	plans := NewPlanStore(pool)
	workspaceID, otherWorkspaceID := randomID(t), randomID(t)

	seedStatsDelivery(t, ctx, plans, store, workspaceID, domain.StateScheduled, 0)
	seedStatsDelivery(t, ctx, plans, store, workspaceID, domain.StateSending, 0)
	seedStatsDelivery(t, ctx, plans, store, workspaceID, domain.StateSending, 3)
	seedStatsDelivery(t, ctx, plans, store, workspaceID, domain.StateSucceeded, 1)
	seedStatsDelivery(t, ctx, plans, store, workspaceID, domain.StateFailed, 2)
	seedStatsDelivery(t, ctx, plans, store, workspaceID, domain.StateSuppressed, 0)
	// Another workspace's rows must not leak into the counts.
	seedStatsDelivery(t, ctx, plans, store, otherWorkspaceID, domain.StateScheduled, 0)
	seedStatsDelivery(t, ctx, plans, store, otherWorkspaceID, domain.StateSending, 0)

	counts, err := store.Stats(ctx, workspaceID)
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	want := dto.DeliveryCounts{Scheduled: 1, Sending: 1, Retrying: 1, Succeeded: 1, Failed: 1, Suppressed: 1}
	if counts != want {
		t.Fatalf("Stats() = %#v, want %#v", counts, want)
	}

	otherCounts, err := store.Stats(ctx, otherWorkspaceID)
	if err != nil {
		t.Fatalf("Stats(other) error = %v", err)
	}
	wantOther := dto.DeliveryCounts{Scheduled: 1, Sending: 1}
	if otherCounts != wantOther {
		t.Fatalf("Stats(other) = %#v, want %#v", otherCounts, wantOther)
	}

	emptyCounts, err := store.Stats(ctx, randomID(t))
	if err != nil {
		t.Fatalf("Stats(empty) error = %v", err)
	}
	if emptyCounts != (dto.DeliveryCounts{}) {
		t.Fatalf("Stats(empty) = %#v, want all zero", emptyCounts)
	}
}

// seedListDelivery saves one delivery for a fresh todo with an explicit
// created_at and state so ordering and filtering are deterministic.
func seedListDelivery(t *testing.T, ctx context.Context, plans *PlanStore, deliveries *DeliveryStore, workspaceID string, createdAt time.Time, state domain.DeliveryState, attemptCount int) domain.ReminderDelivery {
	t.Helper()
	todoID := randomID(t)
	plan := seedPlanFixture(t, ctx, plans, workspaceID, todoID, 1)
	delivery := newDeliveryFixture(t, workspaceID, randomID(t), todoID, 1, plan.ID, "email", testNow.Add(time.Hour))
	delivery.CreatedAt = createdAt
	delivery.State = state
	delivery.AttemptCount = attemptCount
	if err := deliveries.Save(ctx, delivery); err != nil {
		t.Fatalf("Save(list fixture) error = %v", err)
	}
	return delivery
}

func TestDeliveryStoreListOrderFilterLimitOffset(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewDeliveryStore(pool)
	plans := NewPlanStore(pool)
	workspaceID := randomID(t)

	d1 := seedListDelivery(t, ctx, plans, store, workspaceID, testNow.Add(1*time.Minute), domain.StateScheduled, 0)
	d2 := seedListDelivery(t, ctx, plans, store, workspaceID, testNow.Add(2*time.Minute), domain.StateSending, 0)
	d3 := seedListDelivery(t, ctx, plans, store, workspaceID, testNow.Add(3*time.Minute), domain.StateSending, 2)
	d4 := seedListDelivery(t, ctx, plans, store, workspaceID, testNow.Add(4*time.Minute), domain.StateSucceeded, 1)
	d5 := seedListDelivery(t, ctx, plans, store, workspaceID, testNow.Add(5*time.Minute), domain.StateFailed, 1)
	// Another workspace's delivery must never appear in this listing.
	seedListDelivery(t, ctx, plans, store, randomID(t), testNow.Add(6*time.Minute), domain.StateScheduled, 0)

	ids := func(t *testing.T, rows []domain.ReminderDelivery) []string {
		t.Helper()
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			out = append(out, row.ID)
		}
		return out
	}

	// No filter: newest first.
	all, err := store.List(ctx, workspaceID, dto.DeliveryFilter{Limit: 50, Offset: 0})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if !reflect.DeepEqual(ids(t, all), []string{d5.ID, d4.ID, d3.ID, d2.ID, d1.ID}) {
		t.Fatalf("List() order = %v, want newest first", ids(t, all))
	}

	// The retrying alias matches only sending rows with at least one retry.
	retrying, err := store.List(ctx, workspaceID, dto.DeliveryFilter{Status: "retrying", Limit: 50, Offset: 0})
	if err != nil {
		t.Fatalf("List(retrying) error = %v", err)
	}
	if !reflect.DeepEqual(ids(t, retrying), []string{d3.ID}) {
		t.Fatalf("List(retrying) = %v, want [%s]", ids(t, retrying), d3.ID)
	}

	// Plain sending matches every sending row regardless of attempt count.
	sending, err := store.List(ctx, workspaceID, dto.DeliveryFilter{Status: "sending", Limit: 50, Offset: 0})
	if err != nil {
		t.Fatalf("List(sending) error = %v", err)
	}
	if !reflect.DeepEqual(ids(t, sending), []string{d3.ID, d2.ID}) {
		t.Fatalf("List(sending) = %v, want [%s %s]", ids(t, sending), d3.ID, d2.ID)
	}

	failed, err := store.List(ctx, workspaceID, dto.DeliveryFilter{Status: "failed", Limit: 50, Offset: 0})
	if err != nil {
		t.Fatalf("List(failed) error = %v", err)
	}
	if !reflect.DeepEqual(ids(t, failed), []string{d5.ID}) {
		t.Fatalf("List(failed) = %v, want [%s]", ids(t, failed), d5.ID)
	}

	// Limit and offset page through the newest-first ordering.
	page, err := store.List(ctx, workspaceID, dto.DeliveryFilter{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("List(page 1) error = %v", err)
	}
	if !reflect.DeepEqual(ids(t, page), []string{d5.ID, d4.ID}) {
		t.Fatalf("List(page 1) = %v, want [%s %s]", ids(t, page), d5.ID, d4.ID)
	}
	nextPage, err := store.List(ctx, workspaceID, dto.DeliveryFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("List(page 2) error = %v", err)
	}
	if !reflect.DeepEqual(ids(t, nextPage), []string{d3.ID, d2.ID}) {
		t.Fatalf("List(page 2) = %v, want [%s %s]", ids(t, nextPage), d3.ID, d2.ID)
	}

	// Another workspace lists nothing.
	other, err := store.List(ctx, randomID(t), dto.DeliveryFilter{Limit: 50, Offset: 0})
	if err != nil {
		t.Fatalf("List(other workspace) error = %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("List(other workspace) = %v, want none", ids(t, other))
	}
}

func TestDeliveryStoreByProviderMessageID(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewDeliveryStore(pool)
	plans := NewPlanStore(pool)
	workspaceA, workspaceB := randomID(t), randomID(t)

	todoA := randomID(t)
	planA := seedPlanFixture(t, ctx, plans, workspaceA, todoA, 1)
	deliveryA := newDeliveryFixture(t, workspaceA, randomID(t), todoA, 1, planA.ID, "email", testNow.Add(time.Hour))
	if err := deliveryA.MarkSending(testNow); err != nil {
		t.Fatalf("MarkSending() error = %v", err)
	}
	if err := deliveryA.MarkSucceeded("provider-message-alpha", testNow.Add(time.Minute)); err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}
	if err := store.Save(ctx, deliveryA); err != nil {
		t.Fatalf("Save(workspace A) error = %v", err)
	}

	todoB := randomID(t)
	planB := seedPlanFixture(t, ctx, plans, workspaceB, todoB, 1)
	deliveryB := newDeliveryFixture(t, workspaceB, randomID(t), todoB, 1, planB.ID, "sms", testNow.Add(time.Hour))
	if err := deliveryB.MarkSending(testNow); err != nil {
		t.Fatalf("MarkSending() error = %v", err)
	}
	if err := deliveryB.MarkSucceeded("provider-message-beta", testNow.Add(time.Minute)); err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}
	if err := store.Save(ctx, deliveryB); err != nil {
		t.Fatalf("Save(workspace B) error = %v", err)
	}

	// Provider-keyed lookup (documented D6 exception): not workspace-scoped.
	got, err := store.ByProviderMessageID(ctx, "provider-message-alpha")
	if err != nil {
		t.Fatalf("ByProviderMessageID(alpha) error = %v", err)
	}
	if !reflect.DeepEqual(got, deliveryA) {
		t.Fatalf("ByProviderMessageID(alpha) = %#v, want %#v", got, deliveryA)
	}
	if got, err = store.ByProviderMessageID(ctx, "provider-message-beta"); err != nil {
		t.Fatalf("ByProviderMessageID(beta) error = %v", err)
	}
	if got.ID != deliveryB.ID || got.WorkspaceID != workspaceB {
		t.Fatalf("ByProviderMessageID(beta) = %#v, want delivery %s in workspace %s", got, deliveryB.ID, workspaceB)
	}

	if _, err := store.ByProviderMessageID(ctx, "provider-message-missing"); !errors.Is(err, domain.ErrDeliveryNotFound) {
		t.Fatalf("ByProviderMessageID(missing) error = %v, want ErrDeliveryNotFound", err)
	}
}

func TestDeliveryStoreJoinsAmbientTransaction(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewDeliveryStore(pool)
	plans := NewPlanStore(pool)
	runner := database.NewTxRunner(pool)
	workspaceID, ownerUserID, todoID := randomID(t), randomID(t), randomID(t)
	plan := seedPlanFixture(t, ctx, plans, workspaceID, todoID, 1)

	// A failing unit of work rolls the delivery back.
	delivery := newDeliveryFixture(t, workspaceID, ownerUserID, todoID, 1, plan.ID, "email", testNow.Add(time.Hour))
	failErr := errors.New("boom")
	err := runner.Run(ctx, func(txCtx context.Context) error {
		if err := store.Save(txCtx, delivery); err != nil {
			return err
		}
		return failErr
	})
	if !errors.Is(err, failErr) {
		t.Fatalf("Run() error = %v, want failErr", err)
	}
	if _, err := store.ByIdempotencyKey(ctx, workspaceID, delivery.IdempotencyKey); !errors.Is(err, domain.ErrDeliveryNotFound) {
		t.Fatalf("ByIdempotencyKey() after rollback error = %v, want ErrDeliveryNotFound", err)
	}

	// A successful unit of work commits Save, SetProviderJobID, and Update
	// together. The job ID is written before the Update (whose in-memory struct
	// still carries no job ID) to prove the Update cannot overwrite it.
	if err := runner.Run(ctx, func(txCtx context.Context) error {
		if err := store.Save(txCtx, delivery); err != nil {
			return err
		}
		if err := store.SetProviderJobID(txCtx, workspaceID, delivery.ID, 777); err != nil {
			return err
		}
		if err := delivery.MarkSending(testNow.Add(time.Minute)); err != nil {
			return err
		}
		return store.Update(txCtx, delivery)
	}); err != nil {
		t.Fatalf("Run(commit) error = %v", err)
	}
	got, err := store.ByIdempotencyKey(ctx, workspaceID, delivery.IdempotencyKey)
	if err != nil {
		t.Fatalf("ByIdempotencyKey() after commit error = %v", err)
	}
	jobID := int64(777)
	want := delivery
	want.ProviderJobID = &jobID
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after commit = %#v, want %#v", got, want)
	}
}

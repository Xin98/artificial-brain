package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

var fixedNow = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

// newPlanHandler wires the handler with a deterministic ID sequence: the
// first call builds the plan, the following calls build its deliveries.
func newPlanHandler(store *fakePlanStore, deliveries *fakeDeliveryStore, scheduler *fakeScheduler) *PlanReminderHandler {
	ids := []string{"plan-1", "delivery-1", "delivery-2", "delivery-3"}
	next := 0
	return &PlanReminderHandler{
		Plans:      store,
		Deliveries: deliveries,
		Scheduler:  scheduler,
		NewID: func() string {
			next++
			return ids[next-1]
		},
		Now: func() time.Time { return fixedNow },
	}
}

func planRequest() dto.PlanRequest {
	return dto.PlanRequest{
		WorkspaceID:         "ws-1",
		TodoID:              "todo-1",
		OwnerUserID:         "user-1",
		Title:               "提交周报",
		TodoReminderVersion: 1,
		ScheduledAtUTC:      fixedNow.Add(2 * time.Hour),
		Channels:            []string{"sms", "email"},
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newRevokeHandler(store *fakePlanStore, deliveries *fakeDeliveryStore, scheduler *fakeScheduler, log *slog.Logger) *RevokePlansHandler {
	return &RevokePlansHandler{
		Plans:      store,
		Deliveries: deliveries,
		Scheduler:  scheduler,
		Log:        log,
		Now:        func() time.Time { return fixedNow },
	}
}

func TestPlanReminderPersistsThenSchedulesWithIdenticalFields(t *testing.T) {
	store := newFakePlanStore()
	deliveries := newFakeDeliveryStore()
	scheduler := newFakeScheduler()
	handler := newPlanHandler(store, deliveries, scheduler)

	if err := handler.Handle(context.Background(), planRequest()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(store.saved) != 1 {
		t.Fatalf("saved plans = %d, want 1", len(store.saved))
	}
	plan := store.saved[0]
	if plan.ID != "plan-1" || plan.WorkspaceID != "ws-1" || plan.TodoID != "todo-1" {
		t.Fatalf("saved plan identity = %#v", plan)
	}
	if plan.TodoReminderVersion != 1 || !plan.ScheduledAtUTC.Equal(fixedNow.Add(2*time.Hour)) {
		t.Fatalf("saved plan schedule = %#v", plan)
	}
	if plan.Status != domain.StatusPlanned || !plan.CreatedAt.Equal(fixedNow) {
		t.Fatalf("saved plan status/created = %#v", plan)
	}
	if len(scheduler.jobs) != 1 {
		t.Fatalf("scheduled jobs = %d, want 1", len(scheduler.jobs))
	}
	job := scheduler.jobs[0]
	if job.PlanID != plan.ID || job.WorkspaceID != plan.WorkspaceID || job.TodoID != plan.TodoID {
		t.Fatalf("job identity = %#v, plan = %#v", job, plan)
	}
	if job.TodoReminderVersion != plan.TodoReminderVersion || !job.ScheduledAtUTC.Equal(plan.ScheduledAtUTC) {
		t.Fatalf("job schedule = %#v, plan = %#v", job, plan)
	}
	if len(job.Channels) != len(plan.RequestedChannels) {
		t.Fatalf("job.Channels = %#v, plan channels = %#v", job.Channels, plan.RequestedChannels)
	}
	if job.OwnerUserID != "user-1" {
		t.Fatalf("job.OwnerUserID = %q, want user-1", job.OwnerUserID)
	}
}

func TestPlanReminderSavesDeliveryPerChannelThenWritesBackJobIDs(t *testing.T) {
	store := newFakePlanStore()
	deliveries := newFakeDeliveryStore()
	scheduler := newFakeScheduler()
	scheduler.scheduled = []ports.ScheduledChannel{
		{Channel: "sms", JobID: 10},
		{Channel: "email", JobID: 11},
	}
	handler := newPlanHandler(store, deliveries, scheduler)

	if err := handler.Handle(context.Background(), planRequest()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(store.saved) != 1 {
		t.Fatalf("saved plans = %d, want 1", len(store.saved))
	}
	if len(deliveries.saved) != 2 {
		t.Fatalf("saved deliveries = %d, want 2", len(deliveries.saved))
	}
	wantChannels := []string{"sms", "email"}
	wantIDs := []string{"delivery-1", "delivery-2"}
	for index, delivery := range deliveries.saved {
		if delivery.ID != wantIDs[index] || delivery.Channel != wantChannels[index] {
			t.Fatalf("delivery[%d] identity = %#v, want id %s channel %s", index, delivery, wantIDs[index], wantChannels[index])
		}
		if delivery.PlanID != "plan-1" || delivery.WorkspaceID != "ws-1" || delivery.TodoID != "todo-1" || delivery.TodoReminderVersion != 1 {
			t.Fatalf("delivery[%d] plan linkage = %#v", index, delivery)
		}
		if delivery.OwnerUserID != "user-1" {
			t.Fatalf("delivery[%d].OwnerUserID = %q, want user-1 snapshot", index, delivery.OwnerUserID)
		}
		if delivery.TodoTitleSnapshot != "提交周报" {
			t.Fatalf("delivery[%d].TodoTitleSnapshot = %q, want title snapshot", index, delivery.TodoTitleSnapshot)
		}
		wantKey := domain.IdempotencyKeyFor("ws-1", "todo-1", 1, wantChannels[index])
		if delivery.IdempotencyKey != wantKey {
			t.Fatalf("delivery[%d].IdempotencyKey = %q, want %q", index, delivery.IdempotencyKey, wantKey)
		}
		if delivery.State != domain.StateScheduled {
			t.Fatalf("delivery[%d].State = %q, want scheduled", index, delivery.State)
		}
		if !delivery.ScheduledAt.Equal(fixedNow.Add(2*time.Hour)) || !delivery.CreatedAt.Equal(fixedNow) {
			t.Fatalf("delivery[%d] times = %#v", index, delivery)
		}
	}
	if len(scheduler.jobs) != 1 {
		t.Fatalf("scheduled jobs = %d, want exactly 1 fan-out call", len(scheduler.jobs))
	}
	if len(deliveries.setProviderJobIDCalls) != 2 {
		t.Fatalf("SetProviderJobID calls = %d, want 2", len(deliveries.setProviderJobIDCalls))
	}
	wantWritebacks := []providerJobIDCall{
		{workspaceID: "ws-1", deliveryID: "delivery-1", jobID: 10},
		{workspaceID: "ws-1", deliveryID: "delivery-2", jobID: 11},
	}
	for index, call := range deliveries.setProviderJobIDCalls {
		if call != wantWritebacks[index] {
			t.Fatalf("SetProviderJobID call[%d] = %#v, want %#v", index, call, wantWritebacks[index])
		}
	}
}

func TestPlanReminderSchedulerErrorFailsHandler(t *testing.T) {
	store := newFakePlanStore()
	deliveries := newFakeDeliveryStore()
	scheduler := newFakeScheduler()
	scheduler.scheduleErr = errScheduleFailed
	handler := newPlanHandler(store, deliveries, scheduler)

	if err := handler.Handle(context.Background(), planRequest()); !errors.Is(err, errScheduleFailed) {
		t.Fatalf("Handle() error = %v, want errScheduleFailed", err)
	}
	if len(deliveries.setProviderJobIDCalls) != 0 {
		t.Fatalf("SetProviderJobID calls = %d, want 0 after scheduler failure", len(deliveries.setProviderJobIDCalls))
	}
}

func TestPlanReminderStoreErrorFailsHandlerWithoutScheduling(t *testing.T) {
	store := newFakePlanStore()
	store.saveErr = errSaveFailed
	deliveries := newFakeDeliveryStore()
	scheduler := newFakeScheduler()
	handler := newPlanHandler(store, deliveries, scheduler)

	if err := handler.Handle(context.Background(), planRequest()); !errors.Is(err, errSaveFailed) {
		t.Fatalf("Handle() error = %v, want errSaveFailed", err)
	}
	if len(scheduler.jobs) != 0 {
		t.Fatalf("scheduled jobs = %d, want 0 after store failure", len(scheduler.jobs))
	}
	if len(deliveries.saved) != 0 {
		t.Fatalf("saved deliveries = %d, want 0 after store failure", len(deliveries.saved))
	}
}

func TestPlanReminderDuplicateIsIdempotentAndSkipsScheduling(t *testing.T) {
	store := newFakePlanStore()
	store.saveErr = domain.ErrPlanExists
	deliveries := newFakeDeliveryStore()
	scheduler := newFakeScheduler()
	handler := newPlanHandler(store, deliveries, scheduler)

	if err := handler.Handle(context.Background(), planRequest()); err != nil {
		t.Fatalf("Handle() on duplicate error = %v, want nil", err)
	}
	if len(scheduler.jobs) != 0 {
		t.Fatalf("scheduled jobs = %d, want 0 for duplicate plan", len(scheduler.jobs))
	}
	if len(deliveries.saved) != 0 {
		t.Fatalf("saved deliveries = %d, want 0 for duplicate plan", len(deliveries.saved))
	}
	if len(deliveries.setProviderJobIDCalls) != 0 {
		t.Fatalf("SetProviderJobID calls = %d, want 0 for duplicate plan", len(deliveries.setProviderJobIDCalls))
	}
}

func TestPlanReminderEmptyChannelsPlansOnly(t *testing.T) {
	store := newFakePlanStore()
	deliveries := newFakeDeliveryStore()
	scheduler := newFakeScheduler()
	handler := newPlanHandler(store, deliveries, scheduler)

	request := planRequest()
	request.Channels = nil
	if err := handler.Handle(context.Background(), request); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(store.saved) != 1 {
		t.Fatalf("saved plans = %d, want 1", len(store.saved))
	}
	if len(deliveries.saved) != 0 {
		t.Fatalf("saved deliveries = %d, want 0 without channels", len(deliveries.saved))
	}
	if len(deliveries.setProviderJobIDCalls) != 0 {
		t.Fatalf("SetProviderJobID calls = %d, want 0 without channels", len(deliveries.setProviderJobIDCalls))
	}
	// The scheduler is still consulted (zero channels fan out to zero jobs)
	// so a downed scheduler fails the plan exactly like the ITER-0002 seam.
	if len(scheduler.jobs) != 1 || len(scheduler.jobs[0].Channels) != 0 {
		t.Fatalf("scheduled jobs = %#v, want one job carrying no channels", scheduler.jobs)
	}
}

func TestPlanReminderRejectsMissingSchedule(t *testing.T) {
	store := newFakePlanStore()
	deliveries := newFakeDeliveryStore()
	scheduler := newFakeScheduler()
	handler := newPlanHandler(store, deliveries, scheduler)

	request := planRequest()
	request.ScheduledAtUTC = time.Time{}
	if err := handler.Handle(context.Background(), request); !errors.Is(err, domain.ErrMissingSchedule) {
		t.Fatalf("Handle() error = %v, want ErrMissingSchedule", err)
	}
	if len(store.saved) != 0 || len(scheduler.jobs) != 0 || len(deliveries.saved) != 0 {
		t.Fatalf("invalid request persisted %d plans, %d deliveries and scheduled %d jobs",
			len(store.saved), len(deliveries.saved), len(scheduler.jobs))
	}
}

func TestRevokePlansDelegatesCutoffWithInjectedClock(t *testing.T) {
	store := newFakePlanStore()
	handler := newRevokeHandler(store, newFakeDeliveryStore(), newFakeScheduler(), discardLogger())

	err := handler.Handle(context.Background(), dto.RevokeRequest{
		WorkspaceID:         "ws-1",
		TodoID:              "todo-1",
		UpToReminderVersion: 2,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(store.revokeCalls) != 1 {
		t.Fatalf("revoke calls = %d, want 1", len(store.revokeCalls))
	}
	call := store.revokeCalls[0]
	if call.workspaceID != "ws-1" || call.todoID != "todo-1" || call.upToReminderVersion != 2 {
		t.Fatalf("revoke call = %#v", call)
	}
	if !call.now.Equal(fixedNow) {
		t.Fatalf("revoke call now = %v, want injected clock %v", call.now, fixedNow)
	}
}

func TestRevokePlansPropagatesStoreError(t *testing.T) {
	store := newFakePlanStore()
	store.revokeErr = errSaveFailed
	handler := newRevokeHandler(store, newFakeDeliveryStore(), newFakeScheduler(), discardLogger())

	if err := handler.Handle(context.Background(), dto.RevokeRequest{WorkspaceID: "ws-1", TodoID: "todo-1", UpToReminderVersion: 1}); !errors.Is(err, errSaveFailed) {
		t.Fatalf("Handle() error = %v, want errSaveFailed", err)
	}
}

func TestRevokePlansCancelsEveryPlannedJobID(t *testing.T) {
	store := newFakePlanStore()
	deliveries := newFakeDeliveryStore()
	deliveries.plannedJobIDs = []int64{10, 11}
	scheduler := newFakeScheduler()
	handler := newRevokeHandler(store, deliveries, scheduler, discardLogger())

	err := handler.Handle(context.Background(), dto.RevokeRequest{WorkspaceID: "ws-1", TodoID: "todo-1", UpToReminderVersion: 3})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(scheduler.cancelCalls) != 2 || scheduler.cancelCalls[0] != 10 || scheduler.cancelCalls[1] != 11 {
		t.Fatalf("cancel calls = %#v, want [10 11]", scheduler.cancelCalls)
	}
}

func TestRevokePlansSurvivesCancelErrorAndLogsIt(t *testing.T) {
	store := newFakePlanStore()
	deliveries := newFakeDeliveryStore()
	deliveries.plannedJobIDs = []int64{10, 11}
	scheduler := newFakeScheduler()
	scheduler.cancelErr = errCancelFailed
	buffer := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buffer, nil))
	handler := newRevokeHandler(store, deliveries, scheduler, logger)

	err := handler.Handle(context.Background(), dto.RevokeRequest{WorkspaceID: "ws-1", TodoID: "todo-1", UpToReminderVersion: 3})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil despite cancel failures", err)
	}
	if len(scheduler.cancelCalls) != 2 {
		t.Fatalf("cancel calls = %#v, want both jobs attempted", scheduler.cancelCalls)
	}
	logged := buffer.String()
	if !bytes.Contains([]byte(logged), []byte("cancel")) || !bytes.Contains([]byte(logged), []byte("cancel failed")) {
		t.Fatalf("log output = %q, want cancel failure logged", logged)
	}
}

func TestRevokePlansWithoutPlannedJobsSkipsCancel(t *testing.T) {
	store := newFakePlanStore()
	scheduler := newFakeScheduler()
	handler := newRevokeHandler(store, newFakeDeliveryStore(), scheduler, discardLogger())

	if err := handler.Handle(context.Background(), dto.RevokeRequest{WorkspaceID: "ws-1", TodoID: "todo-1", UpToReminderVersion: 1}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(scheduler.cancelCalls) != 0 {
		t.Fatalf("cancel calls = %#v, want none without planned jobs", scheduler.cancelCalls)
	}
}

package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

var fixedNow = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

func newPlanHandler(store *fakePlanStore, scheduler *fakeScheduler) *PlanReminderHandler {
	return &PlanReminderHandler{
		Plans:     store,
		Scheduler: scheduler,
		NewID:     func() string { return "plan-1" },
		Now:       func() time.Time { return fixedNow },
	}
}

func planRequest() dto.PlanRequest {
	return dto.PlanRequest{
		WorkspaceID:         "ws-1",
		TodoID:              "todo-1",
		TodoReminderVersion: 1,
		ScheduledAtUTC:      fixedNow.Add(2 * time.Hour),
		Channels:            []string{"sms", "email"},
	}
}

func TestPlanReminderPersistsThenSchedulesWithIdenticalFields(t *testing.T) {
	store := newFakePlanStore()
	scheduler := newFakeScheduler()
	handler := newPlanHandler(store, scheduler)

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
}

func TestPlanReminderSchedulerErrorFailsHandler(t *testing.T) {
	store := newFakePlanStore()
	scheduler := newFakeScheduler()
	scheduler.scheduleErr = errScheduleFailed
	handler := newPlanHandler(store, scheduler)

	if err := handler.Handle(context.Background(), planRequest()); !errors.Is(err, errScheduleFailed) {
		t.Fatalf("Handle() error = %v, want errScheduleFailed", err)
	}
}

func TestPlanReminderStoreErrorFailsHandlerWithoutScheduling(t *testing.T) {
	store := newFakePlanStore()
	store.saveErr = errSaveFailed
	scheduler := newFakeScheduler()
	handler := newPlanHandler(store, scheduler)

	if err := handler.Handle(context.Background(), planRequest()); !errors.Is(err, errSaveFailed) {
		t.Fatalf("Handle() error = %v, want errSaveFailed", err)
	}
	if len(scheduler.jobs) != 0 {
		t.Fatalf("scheduled jobs = %d, want 0 after store failure", len(scheduler.jobs))
	}
}

func TestPlanReminderDuplicateIsIdempotentAndSkipsScheduling(t *testing.T) {
	store := newFakePlanStore()
	store.saveErr = domain.ErrPlanExists
	scheduler := newFakeScheduler()
	handler := newPlanHandler(store, scheduler)

	if err := handler.Handle(context.Background(), planRequest()); err != nil {
		t.Fatalf("Handle() on duplicate error = %v, want nil", err)
	}
	if len(scheduler.jobs) != 0 {
		t.Fatalf("scheduled jobs = %d, want 0 for duplicate plan", len(scheduler.jobs))
	}
}

func TestPlanReminderRejectsMissingSchedule(t *testing.T) {
	store := newFakePlanStore()
	scheduler := newFakeScheduler()
	handler := newPlanHandler(store, scheduler)

	request := planRequest()
	request.ScheduledAtUTC = time.Time{}
	if err := handler.Handle(context.Background(), request); !errors.Is(err, domain.ErrMissingSchedule) {
		t.Fatalf("Handle() error = %v, want ErrMissingSchedule", err)
	}
	if len(store.saved) != 0 || len(scheduler.jobs) != 0 {
		t.Fatalf("invalid request persisted %d plans and scheduled %d jobs", len(store.saved), len(scheduler.jobs))
	}
}

func TestRevokePlansDelegatesCutoffWithInjectedClock(t *testing.T) {
	store := newFakePlanStore()
	handler := &RevokePlansHandler{Plans: store, Now: func() time.Time { return fixedNow }}

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
	handler := &RevokePlansHandler{Plans: store, Now: func() time.Time { return fixedNow }}

	if err := handler.Handle(context.Background(), dto.RevokeRequest{WorkspaceID: "ws-1", TodoID: "todo-1", UpToReminderVersion: 1}); !errors.Is(err, errSaveFailed) {
		t.Fatalf("Handle() error = %v, want errSaveFailed", err)
	}
}

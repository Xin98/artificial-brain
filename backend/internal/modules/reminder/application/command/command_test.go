package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
		Reason:              "todo_completed",
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

	if err := handler.Handle(context.Background(), dto.RevokeRequest{WorkspaceID: "ws-1", TodoID: "todo-1", UpToReminderVersion: 1, Reason: "todo_completed"}); !errors.Is(err, errSaveFailed) {
		t.Fatalf("Handle() error = %v, want errSaveFailed", err)
	}
}

func TestRevokePlansCancelsEveryPlannedJobID(t *testing.T) {
	store := newFakePlanStore()
	deliveries := newFakeDeliveryStore()
	deliveries.plannedJobIDs = []int64{10, 11}
	scheduler := newFakeScheduler()
	handler := newRevokeHandler(store, deliveries, scheduler, discardLogger())

	err := handler.Handle(context.Background(), dto.RevokeRequest{WorkspaceID: "ws-1", TodoID: "todo-1", UpToReminderVersion: 3, Reason: "todo_completed"})
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
	deliveries.seed(scheduledEmailDelivery())
	scheduler := newFakeScheduler()
	scheduler.cancelErr = errCancelFailed
	buffer := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buffer, nil))
	handler := newRevokeHandler(store, deliveries, scheduler, logger)

	err := handler.Handle(context.Background(), dto.RevokeRequest{WorkspaceID: "ws-1", TodoID: "todo-1", UpToReminderVersion: 3, Reason: "todo_completed"})
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
	// Suppression must not be skipped because a cancel errored.
	delivery := deliveries.rows[emailDeliveryKey()]
	if delivery.State != domain.StateSuppressed || delivery.SuppressionReason == nil || *delivery.SuppressionReason != domain.ReasonTodoCompleted {
		t.Fatalf("delivery = %#v, want suppressed(todo_completed) despite cancel failures", delivery)
	}
}

func TestRevokePlansWithoutPlannedJobsSkipsCancel(t *testing.T) {
	store := newFakePlanStore()
	scheduler := newFakeScheduler()
	handler := newRevokeHandler(store, newFakeDeliveryStore(), scheduler, discardLogger())

	if err := handler.Handle(context.Background(), dto.RevokeRequest{WorkspaceID: "ws-1", TodoID: "todo-1", UpToReminderVersion: 1, Reason: "todo_deleted"}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(scheduler.cancelCalls) != 0 {
		t.Fatalf("cancel calls = %#v, want none without planned jobs", scheduler.cancelCalls)
	}
}

// sendFixture wires a SendReminderHandler against recording fakes with a
// fixed clock. By default the plan exists and is planned, the todo is active
// at the plan's reminder version, the channel is usable, and both notifiers
// succeed — so a test only perturbs the branch it exercises.
type sendFixture struct {
	plans      *fakePlanStore
	deliveries *fakeDeliveryStore
	todos      *fakeTodoReader
	channels   *fakeChannelResolver
	email      *fakeNotifier
	sms        *fakeNotifier
	logBuffer  *bytes.Buffer
	handler    *SendReminderHandler
}

func newSendFixture() *sendFixture {
	plans := newFakePlanStore()
	plans.hasPlan = true
	plans.plan = plannedReminderPlan()
	deliveries := newFakeDeliveryStore()
	todos := newFakeTodoReader()
	todos.view = activeTodoView()
	channels := newFakeChannelResolver()
	channels.endpoint = dto.ChannelEndpoint{Address: "user-1@example.com", Usable: true}
	email := newFakeNotifier()
	email.result = dto.SendResult{ProviderMessageID: "prov-email-1"}
	sms := newFakeNotifier()
	sms.result = dto.SendResult{ProviderMessageID: "prov-sms-1"}
	buffer := &bytes.Buffer{}
	nextID := 0
	fixture := &sendFixture{
		plans:      plans,
		deliveries: deliveries,
		todos:      todos,
		channels:   channels,
		email:      email,
		sms:        sms,
		logBuffer:  buffer,
	}
	fixture.handler = &SendReminderHandler{
		Plans:      plans,
		Deliveries: deliveries,
		Todos:      todos,
		Channels:   channels,
		Email:      email,
		Sms:        sms,
		Log:        slog.New(slog.NewTextHandler(buffer, nil)),
		NewID: func() string {
			nextID++
			return fmt.Sprintf("delivery-new-%d", nextID)
		},
		Now: func() time.Time { return fixedNow },
	}
	return fixture
}

func plannedReminderPlan() domain.ReminderPlan {
	return domain.ReminderPlan{
		ID:                  "plan-1",
		WorkspaceID:         "ws-1",
		TodoID:              "todo-1",
		TodoReminderVersion: 2,
		ScheduledAtUTC:      fixedNow.Add(-time.Minute),
		RequestedChannels:   []string{"email"},
		Status:              domain.StatusPlanned,
		CreatedAt:           fixedNow.Add(-time.Hour),
	}
}

func activeTodoView() dto.TodoView {
	return dto.TodoView{Title: "提交周报", Status: "pending", ReminderVersion: 2, OwnerUserID: "user-1"}
}

func scheduledEmailDelivery() domain.ReminderDelivery {
	delivery, err := domain.NewDelivery("delivery-1", "ws-1", "user-1", "todo-1", 2, "plan-1", "email",
		"提交周报", fixedNow.Add(-time.Minute), fixedNow.Add(-time.Hour))
	if err != nil {
		panic(err)
	}
	return delivery
}

func emailDeliveryKey() string {
	return domain.IdempotencyKeyFor("ws-1", "todo-1", 2, "email")
}

func sendRequestFixture() SendRequest {
	return SendRequest{WorkspaceID: "ws-1", OwnerUserID: "user-1", PlanID: "plan-1", Channel: "email"}
}

func assertNoNotifierCalls(t *testing.T, fixture *sendFixture) {
	t.Helper()
	if len(fixture.email.calls) != 0 || len(fixture.sms.calls) != 0 {
		t.Fatalf("notifier calls = %d email, %d sms; want none", len(fixture.email.calls), len(fixture.sms.calls))
	}
}

func TestSendReminderMissingPlanIsIgnoredAndLogged(t *testing.T) {
	fixture := newSendFixture()
	fixture.plans.hasPlan = false

	if err := fixture.handler.Handle(context.Background(), sendRequestFixture()); err != nil {
		t.Fatalf("Handle() error = %v, want nil for missing plan", err)
	}
	if len(fixture.plans.getCalls) != 1 || fixture.plans.getCalls[0].workspaceID != "ws-1" || fixture.plans.getCalls[0].planID != "plan-1" {
		t.Fatalf("plan get calls = %#v, want one scoped lookup", fixture.plans.getCalls)
	}
	if len(fixture.todos.getCalls) != 0 {
		t.Fatalf("todo get calls = %#v, want none after missing plan", fixture.todos.getCalls)
	}
	assertNoNotifierCalls(t, fixture)
	if len(fixture.deliveries.saved) != 0 || len(fixture.deliveries.updated) != 0 {
		t.Fatalf("deliveries saved %d updated %d, want none", len(fixture.deliveries.saved), len(fixture.deliveries.updated))
	}
	if !bytes.Contains(fixture.logBuffer.Bytes(), []byte("plan not found")) {
		t.Fatalf("log = %q, want missing plan logged", fixture.logBuffer.String())
	}
}

func TestSendReminderRevokedPlanSuppressesPlanRevoked(t *testing.T) {
	fixture := newSendFixture()
	revokedAt := fixedNow.Add(-30 * time.Minute)
	fixture.plans.plan.Status = domain.StatusRevoked
	fixture.plans.plan.RevokedAt = &revokedAt
	fixture.deliveries.seed(scheduledEmailDelivery())

	if err := fixture.handler.Handle(context.Background(), sendRequestFixture()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	delivery := fixture.deliveries.rows[emailDeliveryKey()]
	if delivery.State != domain.StateSuppressed || delivery.SuppressionReason == nil || *delivery.SuppressionReason != domain.ReasonPlanRevoked {
		t.Fatalf("delivery = %#v, want suppressed(plan_revoked)", delivery)
	}
	if delivery.FinalizedAt == nil || !delivery.FinalizedAt.Equal(fixedNow) {
		t.Fatalf("delivery.FinalizedAt = %v, want fixed clock", delivery.FinalizedAt)
	}
	if len(fixture.todos.getCalls) != 0 {
		t.Fatalf("todo get calls = %#v, want none for revoked plan", fixture.todos.getCalls)
	}
	assertNoNotifierCalls(t, fixture)
}

func TestSendReminderSuppressesOnExecutionTimeReread(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(fixture *sendFixture)
		want   domain.SuppressionReason
	}{
		{"todo deleted", func(f *sendFixture) { f.todos.view.Status = "deleted" }, domain.ReasonTodoDeleted},
		{"todo completed", func(f *sendFixture) { f.todos.view.Status = "completed" }, domain.ReasonTodoCompleted},
		{"version stale", func(f *sendFixture) { f.todos.view.ReminderVersion = 3 }, domain.ReasonVersionStale},
		{"todo not found", func(f *sendFixture) { f.todos.view = dto.TodoView{}; f.todos.err = ports.ErrTodoNotFound }, domain.ReasonTodoDeleted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newSendFixture()
			tt.mutate(fixture)
			fixture.deliveries.seed(scheduledEmailDelivery())

			if err := fixture.handler.Handle(context.Background(), sendRequestFixture()); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			delivery := fixture.deliveries.rows[emailDeliveryKey()]
			if delivery.State != domain.StateSuppressed || delivery.SuppressionReason == nil || *delivery.SuppressionReason != tt.want {
				t.Fatalf("delivery = %#v, want suppressed(%s)", delivery, tt.want)
			}
			assertNoNotifierCalls(t, fixture)
		})
	}
}

func TestSendReminderSuppressionCreatesMissingDeliveryRow(t *testing.T) {
	fixture := newSendFixture()
	fixture.todos.view = dto.TodoView{}
	fixture.todos.err = ports.ErrTodoNotFound

	if err := fixture.handler.Handle(context.Background(), sendRequestFixture()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(fixture.deliveries.saved) != 1 {
		t.Fatalf("saved deliveries = %d, want defensive insert", len(fixture.deliveries.saved))
	}
	delivery := fixture.deliveries.saved[0]
	if delivery.State != domain.StateSuppressed || delivery.SuppressionReason == nil || *delivery.SuppressionReason != domain.ReasonTodoDeleted {
		t.Fatalf("saved delivery = %#v, want suppressed(todo_deleted)", delivery)
	}
	if delivery.OwnerUserID != "user-1" || delivery.IdempotencyKey != emailDeliveryKey() {
		t.Fatalf("saved delivery identity = %#v", delivery)
	}
	if delivery.TodoTitleSnapshot == "" {
		t.Fatalf("saved delivery title snapshot empty, want a fallback so audit can render it")
	}
	assertNoNotifierCalls(t, fixture)
}

func TestSendReminderFinalDeliveryIsIdempotent(t *testing.T) {
	fixture := newSendFixture()
	delivery := scheduledEmailDelivery()
	if err := delivery.MarkSending(fixedNow.Add(-time.Minute)); err != nil {
		t.Fatalf("MarkSending() error = %v", err)
	}
	if err := delivery.MarkSucceeded("prov-9", fixedNow.Add(-time.Minute)); err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}
	fixture.deliveries.seed(delivery)

	if err := fixture.handler.Handle(context.Background(), sendRequestFixture()); err != nil {
		t.Fatalf("Handle() error = %v, want nil for final delivery", err)
	}
	assertNoNotifierCalls(t, fixture)
	if len(fixture.deliveries.saved) != 0 || len(fixture.deliveries.updated) != 0 {
		t.Fatalf("deliveries saved %d updated %d, want none for idempotent skip",
			len(fixture.deliveries.saved), len(fixture.deliveries.updated))
	}
}

func TestSendReminderMissingDeliveryDefensiveInsertThenNormalFlow(t *testing.T) {
	fixture := newSendFixture()

	if err := fixture.handler.Handle(context.Background(), sendRequestFixture()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(fixture.deliveries.saved) != 1 {
		t.Fatalf("saved deliveries = %d, want defensive insert", len(fixture.deliveries.saved))
	}
	created := fixture.deliveries.saved[0]
	if created.State != domain.StateScheduled || created.TodoTitleSnapshot != "提交周报" || created.OwnerUserID != "user-1" {
		t.Fatalf("defensively created delivery = %#v", created)
	}
	if !bytes.Contains(fixture.logBuffer.Bytes(), []byte("defensively")) {
		t.Fatalf("log = %q, want defensive insert logged", fixture.logBuffer.String())
	}
	delivery := fixture.deliveries.rows[emailDeliveryKey()]
	if delivery.State != domain.StateSucceeded || delivery.ProviderMessageID == nil || *delivery.ProviderMessageID != "prov-email-1" {
		t.Fatalf("final delivery = %#v, want succeeded with provider message id", delivery)
	}
	if len(fixture.email.calls) != 1 {
		t.Fatalf("email calls = %d, want 1", len(fixture.email.calls))
	}
}

func TestSendReminderUnusableChannelSuppresses(t *testing.T) {
	fixture := newSendFixture()
	fixture.channels.endpoint = dto.ChannelEndpoint{}
	fixture.deliveries.seed(scheduledEmailDelivery())

	if err := fixture.handler.Handle(context.Background(), sendRequestFixture()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	delivery := fixture.deliveries.rows[emailDeliveryKey()]
	if delivery.State != domain.StateSuppressed || delivery.SuppressionReason == nil || *delivery.SuppressionReason != domain.ReasonChannelUnavailable {
		t.Fatalf("delivery = %#v, want suppressed(channel_unavailable)", delivery)
	}
	assertNoNotifierCalls(t, fixture)
	if len(fixture.channels.resolveCalls) != 1 || fixture.channels.resolveCalls[0].userID != "user-1" || fixture.channels.resolveCalls[0].channel != "email" {
		t.Fatalf("resolve calls = %#v, want one owner+channel scoped lookup", fixture.channels.resolveCalls)
	}
}

func TestSendReminderNotifierSuccessMarksSucceeded(t *testing.T) {
	fixture := newSendFixture()
	fixture.deliveries.seed(scheduledEmailDelivery())

	if err := fixture.handler.Handle(context.Background(), sendRequestFixture()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	delivery := fixture.deliveries.rows[emailDeliveryKey()]
	if delivery.State != domain.StateSucceeded {
		t.Fatalf("delivery.State = %q, want succeeded", delivery.State)
	}
	if delivery.ProviderMessageID == nil || *delivery.ProviderMessageID != "prov-email-1" {
		t.Fatalf("delivery.ProviderMessageID = %v, want prov-email-1", delivery.ProviderMessageID)
	}
	if delivery.SubmittedAt == nil || !delivery.SubmittedAt.Equal(fixedNow) || delivery.FinalizedAt == nil || !delivery.FinalizedAt.Equal(fixedNow) {
		t.Fatalf("delivery submitted/finalized = %v/%v, want fixed clock", delivery.SubmittedAt, delivery.FinalizedAt)
	}
	if delivery.AttemptCount != 1 {
		t.Fatalf("delivery.AttemptCount = %d, want 1", delivery.AttemptCount)
	}
	if len(fixture.todos.getCalls) != 1 || fixture.todos.getCalls[0] != (todoGetCall{workspaceID: "ws-1", ownerUserID: "user-1", todoID: "todo-1"}) {
		t.Fatalf("todo get calls = %#v, want one owner-scoped re-read", fixture.todos.getCalls)
	}
	if len(fixture.email.calls) != 1 {
		t.Fatalf("email calls = %d, want 1", len(fixture.email.calls))
	}
	if len(fixture.sms.calls) != 0 {
		t.Fatalf("sms calls = %d, want 0 for an email delivery", len(fixture.sms.calls))
	}
	message := fixture.email.calls[0]
	if message.To != "user-1@example.com" || message.TodoID != "todo-1" || message.Title != "提交周报" || !message.ScheduledAtUTC.Equal(fixedNow.Add(-time.Minute)) {
		t.Fatalf("sent message = %#v", message)
	}
}

func TestSendReminderRoutesChannelToMatchingNotifier(t *testing.T) {
	fixture := newSendFixture()
	delivery, err := domain.NewDelivery("delivery-2", "ws-1", "user-1", "todo-1", 2, "plan-1", "sms",
		"提交周报", fixedNow.Add(-time.Minute), fixedNow.Add(-time.Hour))
	if err != nil {
		t.Fatalf("NewDelivery() error = %v", err)
	}
	fixture.deliveries.seed(delivery)
	request := sendRequestFixture()
	request.Channel = "sms"

	if err := fixture.handler.Handle(context.Background(), request); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(fixture.sms.calls) != 1 || len(fixture.email.calls) != 0 {
		t.Fatalf("notifier calls = %d sms, %d email; want sms only", len(fixture.sms.calls), len(fixture.email.calls))
	}
	smsKey := domain.IdempotencyKeyFor("ws-1", "todo-1", 2, "sms")
	final := fixture.deliveries.rows[smsKey]
	if final.State != domain.StateSucceeded || final.ProviderMessageID == nil || *final.ProviderMessageID != "prov-sms-1" {
		t.Fatalf("sms delivery = %#v, want succeeded via sms notifier", final)
	}
}

func TestSendReminderPermanentErrorDeadLetters(t *testing.T) {
	fixture := newSendFixture()
	fixture.email.err = &ports.PermanentError{Code: "isv.MOBILE_NUMBER_ILLEGAL"}
	fixture.deliveries.seed(scheduledEmailDelivery())

	if err := fixture.handler.Handle(context.Background(), sendRequestFixture()); err != nil {
		t.Fatalf("Handle() error = %v, want nil after dead letter", err)
	}
	delivery := fixture.deliveries.rows[emailDeliveryKey()]
	if delivery.State != domain.StateFailed || delivery.LastErrorCode == nil || *delivery.LastErrorCode != "isv.MOBILE_NUMBER_ILLEGAL" {
		t.Fatalf("delivery = %#v, want failed(isv.MOBILE_NUMBER_ILLEGAL)", delivery)
	}
	if !bytes.Contains(fixture.logBuffer.Bytes(), []byte("dead-lettered")) {
		t.Fatalf("log = %q, want dead-letter event", fixture.logBuffer.String())
	}
	if len(fixture.email.calls) != 1 {
		t.Fatalf("email calls = %d, want 1", len(fixture.email.calls))
	}
}

func TestSendReminderBarePermanentErrorFallsBackToGenericCode(t *testing.T) {
	fixture := newSendFixture()
	fixture.email.err = fmt.Errorf("smtp: 550 mailbox unavailable: %w", ports.ErrPermanent)
	fixture.deliveries.seed(scheduledEmailDelivery())

	if err := fixture.handler.Handle(context.Background(), sendRequestFixture()); err != nil {
		t.Fatalf("Handle() error = %v, want nil after dead letter", err)
	}
	delivery := fixture.deliveries.rows[emailDeliveryKey()]
	if delivery.State != domain.StateFailed || delivery.LastErrorCode == nil || *delivery.LastErrorCode != "permanent_failure" {
		t.Fatalf("delivery = %#v, want failed(permanent_failure) fallback", delivery)
	}
	if !bytes.Contains(fixture.logBuffer.Bytes(), []byte("dead-lettered")) {
		t.Fatalf("log = %q, want dead-letter event", fixture.logBuffer.String())
	}
}

func TestSendReminderWrappedPermanentErrorExtractsProviderCode(t *testing.T) {
	fixture := newSendFixture()
	fixture.email.err = fmt.Errorf("wrap: %w", &ports.PermanentError{Code: "SignatureDoesNotMatch"})
	fixture.deliveries.seed(scheduledEmailDelivery())

	if err := fixture.handler.Handle(context.Background(), sendRequestFixture()); err != nil {
		t.Fatalf("Handle() error = %v, want nil after dead letter", err)
	}
	delivery := fixture.deliveries.rows[emailDeliveryKey()]
	if delivery.State != domain.StateFailed || delivery.LastErrorCode == nil || *delivery.LastErrorCode != "SignatureDoesNotMatch" {
		t.Fatalf("delivery = %#v, want failed(SignatureDoesNotMatch) extracted through wrap", delivery)
	}
}

func TestSendReminderTransientErrorReturnsForRetry(t *testing.T) {
	fixture := newSendFixture()
	fixture.email.err = errProviderTransient
	fixture.deliveries.seed(scheduledEmailDelivery())

	err := fixture.handler.Handle(context.Background(), sendRequestFixture())
	if !errors.Is(err, errProviderTransient) {
		t.Fatalf("Handle() error = %v, want transient error returned for retry", err)
	}
	delivery := fixture.deliveries.rows[emailDeliveryKey()]
	if delivery.State != domain.StateSending || delivery.AttemptCount != 1 || delivery.FinalizedAt != nil {
		t.Fatalf("delivery = %#v, want sending with attempt incremented", delivery)
	}
}

func TestSendReminderTransientErrorOnFinalAttemptDeadLetters(t *testing.T) {
	fixture := newSendFixture()
	fixture.email.err = errProviderTransient
	fixture.deliveries.seed(scheduledEmailDelivery())
	request := sendRequestFixture()
	request.FinalAttempt = true

	if err := fixture.handler.Handle(context.Background(), request); err != nil {
		t.Fatalf("Handle() error = %v, want nil after retry exhaustion", err)
	}
	delivery := fixture.deliveries.rows[emailDeliveryKey()]
	if delivery.State != domain.StateFailed || delivery.LastErrorCode == nil || *delivery.LastErrorCode != "retry_exhausted" {
		t.Fatalf("delivery = %#v, want failed(retry_exhausted)", delivery)
	}
	if !bytes.Contains(fixture.logBuffer.Bytes(), []byte("dead-lettered")) {
		t.Fatalf("log = %q, want dead-letter event", fixture.logBuffer.String())
	}
}

func TestSendReminderSecondRunAfterSuccessIsNoOp(t *testing.T) {
	fixture := newSendFixture()
	fixture.deliveries.seed(scheduledEmailDelivery())

	if err := fixture.handler.Handle(context.Background(), sendRequestFixture()); err != nil {
		t.Fatalf("Handle() first run error = %v", err)
	}
	if err := fixture.handler.Handle(context.Background(), sendRequestFixture()); err != nil {
		t.Fatalf("Handle() second run error = %v, want idempotent nil", err)
	}
	if len(fixture.email.calls) != 1 {
		t.Fatalf("email calls = %d, want exactly 1 across both runs", len(fixture.email.calls))
	}
	if len(fixture.deliveries.updated) != 2 {
		t.Fatalf("updates = %d, want 2 (sending + succeeded) from the first run only", len(fixture.deliveries.updated))
	}
}

func newReceiptHandler(deliveries *fakeDeliveryStore, logger *slog.Logger) *RecordReceiptHandler {
	return &RecordReceiptHandler{
		Deliveries: deliveries,
		Log:        logger,
		Now:        func() time.Time { return fixedNow },
	}
}

func succeededDeliveryWithMessage(providerMessageID string) domain.ReminderDelivery {
	delivery := scheduledEmailDelivery()
	if err := delivery.MarkSending(fixedNow.Add(-time.Minute)); err != nil {
		panic(err)
	}
	if err := delivery.MarkSucceeded(providerMessageID, fixedNow.Add(-time.Minute)); err != nil {
		panic(err)
	}
	return delivery
}

func TestRecordReceiptAppliesOnceToSucceededDelivery(t *testing.T) {
	deliveries := newFakeDeliveryStore()
	deliveries.seed(succeededDeliveryWithMessage("prov-1"))
	handler := newReceiptHandler(deliveries, discardLogger())
	request := ReceiptRequest{ProviderMessageID: "prov-1", Delivered: true}

	if err := handler.Handle(context.Background(), request); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	delivery := deliveries.rows[emailDeliveryKey()]
	if delivery.ReceiptState == nil || *delivery.ReceiptState != domain.ReceiptOK {
		t.Fatalf("delivery.ReceiptState = %v, want received_ok", delivery.ReceiptState)
	}
	if delivery.ReceiptAt == nil || !delivery.ReceiptAt.Equal(fixedNow) {
		t.Fatalf("delivery.ReceiptAt = %v, want fixed clock", delivery.ReceiptAt)
	}
	if len(deliveries.updated) != 1 {
		t.Fatalf("updates = %d, want 1", len(deliveries.updated))
	}

	if err := handler.Handle(context.Background(), request); err != nil {
		t.Fatalf("Handle() second call error = %v", err)
	}
	if len(deliveries.updated) != 1 {
		t.Fatalf("updates after duplicate receipt = %d, want still 1", len(deliveries.updated))
	}
}

func TestRecordReceiptFailedVerdictRecordsErrorCode(t *testing.T) {
	deliveries := newFakeDeliveryStore()
	deliveries.seed(succeededDeliveryWithMessage("prov-2"))
	handler := newReceiptHandler(deliveries, discardLogger())

	err := handler.Handle(context.Background(), ReceiptRequest{ProviderMessageID: "prov-2", Delivered: false, ErrorCode: "number_unreachable"})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	delivery := deliveries.rows[emailDeliveryKey()]
	if delivery.ReceiptState == nil || *delivery.ReceiptState != domain.ReceiptFailed {
		t.Fatalf("delivery.ReceiptState = %v, want received_failed", delivery.ReceiptState)
	}
	if delivery.ReceiptErrorCode == nil || *delivery.ReceiptErrorCode != "number_unreachable" {
		t.Fatalf("delivery.ReceiptErrorCode = %v, want number_unreachable", delivery.ReceiptErrorCode)
	}
}

func TestRecordReceiptIgnoresNonSucceededDelivery(t *testing.T) {
	deliveries := newFakeDeliveryStore()
	delivery := scheduledEmailDelivery()
	providerMessageID := "prov-3"
	delivery.ProviderMessageID = &providerMessageID
	deliveries.seed(delivery)
	handler := newReceiptHandler(deliveries, discardLogger())

	if err := handler.Handle(context.Background(), ReceiptRequest{ProviderMessageID: "prov-3", Delivered: true}); err != nil {
		t.Fatalf("Handle() error = %v, want nil for non-succeeded delivery", err)
	}
	final := deliveries.rows[emailDeliveryKey()]
	if final.ReceiptState != nil {
		t.Fatalf("delivery.ReceiptState = %v, want nil for non-succeeded delivery", final.ReceiptState)
	}
	if len(deliveries.updated) != 0 {
		t.Fatalf("updates = %d, want none", len(deliveries.updated))
	}
}

func TestRecordReceiptUnknownProviderIDIsIgnored(t *testing.T) {
	deliveries := newFakeDeliveryStore()
	buffer := &bytes.Buffer{}
	handler := newReceiptHandler(deliveries, slog.New(slog.NewTextHandler(buffer, nil)))

	err := handler.Handle(context.Background(), ReceiptRequest{ProviderMessageID: "ghost", Delivered: true})
	if err != nil {
		t.Fatalf("Handle() error = %v, want safe ignore", err)
	}
	if len(deliveries.updated) != 0 {
		t.Fatalf("updates = %d, want none", len(deliveries.updated))
	}
	if !bytes.Contains(buffer.Bytes(), []byte("unknown")) {
		t.Fatalf("log = %q, want unknown provider id logged", buffer.String())
	}
}

func sendingSmsDelivery() domain.ReminderDelivery {
	delivery, err := domain.NewDelivery("delivery-sending", "ws-1", "user-1", "todo-1", 2, "plan-1", "sms",
		"提交周报", fixedNow.Add(-time.Minute), fixedNow.Add(-time.Hour))
	if err != nil {
		panic(err)
	}
	if err := delivery.MarkSending(fixedNow.Add(-time.Minute)); err != nil {
		panic(err)
	}
	return delivery
}

func succeededV1EmailDelivery() domain.ReminderDelivery {
	delivery, err := domain.NewDelivery("delivery-final", "ws-1", "user-1", "todo-1", 1, "plan-0", "email",
		"提交周报", fixedNow.Add(-time.Minute), fixedNow.Add(-time.Hour))
	if err != nil {
		panic(err)
	}
	if err := delivery.MarkSending(fixedNow.Add(-time.Minute)); err != nil {
		panic(err)
	}
	if err := delivery.MarkSucceeded("prov-old", fixedNow.Add(-time.Minute)); err != nil {
		panic(err)
	}
	return delivery
}

func TestRevokePlansSuppressesScheduledDeliveriesWithReason(t *testing.T) {
	store := newFakePlanStore()
	deliveries := newFakeDeliveryStore()
	deliveries.seed(scheduledEmailDelivery())
	deliveries.seed(sendingSmsDelivery())
	deliveries.seed(succeededV1EmailDelivery())
	scheduler := newFakeScheduler()
	handler := newRevokeHandler(store, deliveries, scheduler, discardLogger())

	err := handler.Handle(context.Background(), dto.RevokeRequest{
		WorkspaceID:         "ws-1",
		TodoID:              "todo-1",
		UpToReminderVersion: 2,
		Reason:              "todo_completed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	// The scheduled row is finalized as suppressed with the exact reason.
	suppressed := deliveries.rows[emailDeliveryKey()]
	if suppressed.State != domain.StateSuppressed || suppressed.SuppressionReason == nil || *suppressed.SuppressionReason != domain.ReasonTodoCompleted {
		t.Fatalf("scheduled delivery = %#v, want suppressed(todo_completed)", suppressed)
	}
	if suppressed.FinalizedAt == nil || !suppressed.FinalizedAt.Equal(fixedNow) {
		t.Fatalf("suppressed delivery FinalizedAt = %v, want injected clock %v", suppressed.FinalizedAt, fixedNow)
	}
	// The sending row is untouched: an in-flight send keeps the
	// execution-time re-read as its correctness boundary.
	sending := deliveries.rows[domain.IdempotencyKeyFor("ws-1", "todo-1", 2, "sms")]
	if sending.State != domain.StateSending || sending.SuppressionReason != nil {
		t.Fatalf("sending delivery = %#v, want untouched", sending)
	}
	// Final rows never transition again.
	final := deliveries.rows[domain.IdempotencyKeyFor("ws-1", "todo-1", 1, "email")]
	if final.State != domain.StateSucceeded || final.SuppressionReason != nil {
		t.Fatalf("final delivery = %#v, want untouched", final)
	}
	if len(deliveries.updated) != 1 {
		t.Fatalf("delivery updates = %d, want exactly the one scheduled row", len(deliveries.updated))
	}
	if len(deliveries.scheduledForSuppressionArgs) != 1 {
		t.Fatalf("ScheduledForSuppression calls = %#v, want one scoped read", deliveries.scheduledForSuppressionArgs)
	}
	scope := deliveries.scheduledForSuppressionArgs[0]
	if scope.workspaceID != "ws-1" || scope.todoID != "todo-1" || scope.upToReminderVersion != 2 {
		t.Fatalf("ScheduledForSuppression scope = %#v", scope)
	}
}

func TestRevokePlansRejectsInvalidReasonBeforeAnyMutation(t *testing.T) {
	for _, reason := range []string{"", "bogus"} {
		t.Run("reason="+reason, func(t *testing.T) {
			store := newFakePlanStore()
			deliveries := newFakeDeliveryStore()
			deliveries.seed(scheduledEmailDelivery())
			scheduler := newFakeScheduler()
			handler := newRevokeHandler(store, deliveries, scheduler, discardLogger())

			err := handler.Handle(context.Background(), dto.RevokeRequest{
				WorkspaceID:         "ws-1",
				TodoID:              "todo-1",
				UpToReminderVersion: 2,
				Reason:              reason,
			})
			if !errors.Is(err, domain.ErrInvalidSuppressionReason) {
				t.Fatalf("Handle() error = %v, want ErrInvalidSuppressionReason", err)
			}
			if len(store.revokeCalls) != 0 || len(scheduler.cancelCalls) != 0 || len(deliveries.updated) != 0 {
				t.Fatalf("invalid reason mutated state: %d plan revokes, %d cancels, %d delivery updates",
					len(store.revokeCalls), len(scheduler.cancelCalls), len(deliveries.updated))
			}
			seeded := deliveries.rows[emailDeliveryKey()]
			if seeded.State != domain.StateScheduled {
				t.Fatalf("seeded delivery = %#v, want still scheduled", seeded)
			}
		})
	}
}

func TestRevokePlansPropagatesSuppressionReadError(t *testing.T) {
	store := newFakePlanStore()
	deliveries := newFakeDeliveryStore()
	deliveries.scheduledForSuppressionErr = errSaveFailed
	handler := newRevokeHandler(store, deliveries, newFakeScheduler(), discardLogger())

	err := handler.Handle(context.Background(), dto.RevokeRequest{WorkspaceID: "ws-1", TodoID: "todo-1", UpToReminderVersion: 1, Reason: "todo_deleted"})
	if !errors.Is(err, errSaveFailed) {
		t.Fatalf("Handle() error = %v, want the delivery-store read error propagated for rollback", err)
	}
}

func TestRevokePlansPropagatesSuppressionUpdateError(t *testing.T) {
	store := newFakePlanStore()
	deliveries := newFakeDeliveryStore()
	deliveries.seed(scheduledEmailDelivery())
	deliveries.updateErr = errSaveFailed
	handler := newRevokeHandler(store, deliveries, newFakeScheduler(), discardLogger())

	err := handler.Handle(context.Background(), dto.RevokeRequest{WorkspaceID: "ws-1", TodoID: "todo-1", UpToReminderVersion: 2, Reason: "todo_completed"})
	if !errors.Is(err, errSaveFailed) {
		t.Fatalf("Handle() error = %v, want the delivery-store update error propagated for rollback", err)
	}
}

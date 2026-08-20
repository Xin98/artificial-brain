package worker

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

var _ river.Worker[dto.ReminderSendArgs] = (*SendWorker)(nil)

var testNow = time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

// recordingPlanStore records the scoped Get the handler performs and returns
// the configured plan.
type recordingPlanStore struct {
	getWorkspaceID string
	getPlanID      string
	plan           domain.ReminderPlan
	err            error
}

func (s *recordingPlanStore) Save(context.Context, domain.ReminderPlan) error { return nil }

func (s *recordingPlanStore) Get(_ context.Context, workspaceID, planID string) (domain.ReminderPlan, error) {
	s.getWorkspaceID = workspaceID
	s.getPlanID = planID
	return s.plan, s.err
}

func (s *recordingPlanStore) RevokePlanned(context.Context, string, string, int, time.Time) error {
	return nil
}

// recordingTodoReader records the execution-time todo re-read.
type recordingTodoReader struct {
	workspaceID string
	ownerUserID string
	todoID      string
	view        dto.TodoView
	err         error
}

func (r *recordingTodoReader) Get(_ context.Context, workspaceID, ownerUserID, todoID string) (dto.TodoView, error) {
	r.workspaceID = workspaceID
	r.ownerUserID = ownerUserID
	r.todoID = todoID
	return r.view, r.err
}

// stubChannelResolver resolves every channel to the configured endpoint.
type stubChannelResolver struct {
	endpoint dto.ChannelEndpoint
}

func (r stubChannelResolver) Resolve(context.Context, string, string, string) (dto.ChannelEndpoint, error) {
	return r.endpoint, nil
}

// recordingDeliveryStore seeds one delivery row and records every Update so
// tests can inspect the final lifecycle state.
type recordingDeliveryStore struct {
	row     domain.ReminderDelivery
	updates []domain.ReminderDelivery
}

func (s *recordingDeliveryStore) Save(context.Context, domain.ReminderDelivery) error { return nil }

func (s *recordingDeliveryStore) Update(_ context.Context, delivery domain.ReminderDelivery) error {
	s.updates = append(s.updates, delivery)
	s.row = delivery
	return nil
}

func (s *recordingDeliveryStore) ByIdempotencyKey(_ context.Context, workspaceID, key string) (domain.ReminderDelivery, error) {
	if s.row.WorkspaceID == workspaceID && s.row.IdempotencyKey == key {
		return s.row, nil
	}
	return domain.ReminderDelivery{}, domain.ErrDeliveryNotFound
}

func (s *recordingDeliveryStore) ByProviderMessageID(context.Context, string) (domain.ReminderDelivery, error) {
	return domain.ReminderDelivery{}, domain.ErrDeliveryNotFound
}

func (s *recordingDeliveryStore) SetProviderJobID(context.Context, string, string, int64) error {
	return nil
}

func (s *recordingDeliveryStore) PlannedJobIDs(context.Context, string, string, int) ([]int64, error) {
	return nil, nil
}

func (s *recordingDeliveryStore) Stats(context.Context, string) (dto.DeliveryCounts, error) {
	return dto.DeliveryCounts{}, nil
}

func (s *recordingDeliveryStore) List(context.Context, string, dto.DeliveryFilter) ([]domain.ReminderDelivery, error) {
	return nil, nil
}

// recordingNotifier records every message and fails with err when configured.
type recordingNotifier struct {
	messages []dto.ReminderMessage
	err      error
}

func (n *recordingNotifier) Send(_ context.Context, message dto.ReminderMessage) (dto.SendResult, error) {
	n.messages = append(n.messages, message)
	return dto.SendResult{ProviderMessageID: "provider-1"}, n.err
}

type sendHarness struct {
	worker   *SendWorker
	plans    *recordingPlanStore
	todos    *recordingTodoReader
	email    *recordingNotifier
	sms      *recordingNotifier
	delivery *recordingDeliveryStore
}

// newSendHarness wires a real SendReminderHandler over recording fakes so the
// unit test can observe how SendWorker mapped the River job onto the
// application's SendRequest.
func newSendHarness(t *testing.T, maxAttempts int) *sendHarness {
	t.Helper()
	plans := &recordingPlanStore{plan: domain.ReminderPlan{
		ID:                  "plan-1",
		WorkspaceID:         "workspace-1",
		TodoID:              "todo-1",
		TodoReminderVersion: 3,
		ScheduledAtUTC:      testNow,
		RequestedChannels:   []string{"sms"},
		Status:              domain.StatusPlanned,
		CreatedAt:           testNow.Add(-time.Hour),
	}}
	todos := &recordingTodoReader{view: dto.TodoView{
		Title:           "写周报",
		Status:          "pending",
		ReminderVersion: 3,
		OwnerUserID:     "owner-1",
	}}
	delivery := &recordingDeliveryStore{}
	delivery.row = domain.ReminderDelivery{
		ID:                  "delivery-1",
		WorkspaceID:         "workspace-1",
		OwnerUserID:         "owner-1",
		TodoID:              "todo-1",
		TodoReminderVersion: 3,
		PlanID:              "plan-1",
		Channel:             "sms",
		TodoTitleSnapshot:   "写周报",
		IdempotencyKey:      domain.IdempotencyKeyFor("workspace-1", "todo-1", 3, "sms"),
		State:               domain.StateScheduled,
		ScheduledAt:         testNow,
		CreatedAt:           testNow.Add(-time.Hour),
	}
	email := &recordingNotifier{}
	sms := &recordingNotifier{}
	handler := &command.SendReminderHandler{
		Plans:      plans,
		Deliveries: delivery,
		Todos:      todos,
		Channels:   stubChannelResolver{endpoint: dto.ChannelEndpoint{Address: "+8613800000000", Usable: true}},
		Email:      email,
		Sms:        sms,
		Log:        slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{})),
		Now:        func() time.Time { return testNow },
		NewID:      func() string { return "new-id" },
	}
	return &sendHarness{
		worker:   &SendWorker{Handler: handler, MaxAttempts: maxAttempts},
		plans:    plans,
		todos:    todos,
		email:    email,
		sms:      sms,
		delivery: delivery,
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func newJob(attempt int) *river.Job[dto.ReminderSendArgs] {
	return &river.Job[dto.ReminderSendArgs]{
		JobRow: &rivertype.JobRow{Attempt: attempt},
		Args: dto.ReminderSendArgs{
			PlanID:              "plan-1",
			WorkspaceID:         "workspace-1",
			OwnerUserID:         "owner-1",
			TodoID:              "todo-1",
			TodoReminderVersion: 3,
			Channel:             "sms",
		},
	}
}

func TestSendWorkerWorkMapsArgsToSendRequest(t *testing.T) {
	harness := newSendHarness(t, 3)
	transient := errors.New("provider timeout")
	harness.sms.err = transient

	// Attempt 1 of 3 is not the final attempt: the transient error passes
	// through so the queue retries, and the delivery stays non-final.
	err := harness.worker.Work(context.Background(), newJob(1))
	if !errors.Is(err, transient) {
		t.Fatalf("Work() error = %v, want %v", err, transient)
	}

	if harness.plans.getWorkspaceID != "workspace-1" || harness.plans.getPlanID != "plan-1" {
		t.Fatalf("plan read = (%q, %q), want (workspace-1, plan-1)", harness.plans.getWorkspaceID, harness.plans.getPlanID)
	}
	if harness.todos.workspaceID != "workspace-1" || harness.todos.ownerUserID != "owner-1" || harness.todos.todoID != "todo-1" {
		t.Fatalf("todo read = (%q, %q, %q), want (workspace-1, owner-1, todo-1)",
			harness.todos.workspaceID, harness.todos.ownerUserID, harness.todos.todoID)
	}
	if len(harness.sms.messages) != 1 {
		t.Fatalf("sms notifier calls = %d, want 1", len(harness.sms.messages))
	}
	if len(harness.email.messages) != 0 {
		t.Fatalf("email notifier calls = %d, want 0", len(harness.email.messages))
	}
	message := harness.sms.messages[0]
	if message.To != "+8613800000000" || message.Title != "写周报" || !message.ScheduledAtUTC.Equal(testNow) {
		t.Fatalf("notifier message = %#v, want to=+8613800000000 title=写周报 scheduledAt=%v", message, testNow)
	}
	if len(harness.delivery.updates) != 1 || harness.delivery.updates[0].State != domain.StateSending {
		t.Fatalf("delivery updates = %#v, want one sending update", harness.delivery.updates)
	}
	if harness.delivery.updates[0].AttemptCount != 1 {
		t.Fatalf("delivery attempt count = %d, want 1", harness.delivery.updates[0].AttemptCount)
	}
}

func TestSendWorkerWorkFinalAttemptDeadLetters(t *testing.T) {
	harness := newSendHarness(t, 3)
	harness.sms.err = errors.New("provider timeout")

	// Attempt == MaxAttempts marks the final attempt: a transient failure is
	// dead-lettered instead of being returned to the queue.
	if err := harness.worker.Work(context.Background(), newJob(3)); err != nil {
		t.Fatalf("Work() error = %v, want nil (dead-lettered)", err)
	}
	if len(harness.delivery.updates) != 2 {
		t.Fatalf("delivery updates = %d, want 2 (sending then dead letter)", len(harness.delivery.updates))
	}
	final := harness.delivery.updates[len(harness.delivery.updates)-1]
	if final.State != domain.StateFailed {
		t.Fatalf("delivery state = %q, want failed", final.State)
	}
	if final.LastErrorCode == nil || *final.LastErrorCode != "retry_exhausted" {
		t.Fatalf("delivery last error code = %v, want retry_exhausted", final.LastErrorCode)
	}
}

func TestSendWorkerNextRetryDelaySeries(t *testing.T) {
	worker := &SendWorker{MaxAttempts: 3}
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 500 * time.Millisecond},
		{2, time.Second},
		{3, 2 * time.Second},
		{4, 4 * time.Second},
		{7, 32 * time.Second},
		{8, time.Minute}, // 500ms * 2^7 = 64s, capped at 60s
		{9, time.Minute},
		{25, time.Minute},
	}
	for _, testCase := range cases {
		if got := worker.NextRetryDelay(testCase.attempt); got != testCase.want {
			t.Fatalf("NextRetryDelay(%d) = %v, want %v", testCase.attempt, got, testCase.want)
		}
	}
}

func TestSendWorkerNextRetryUsesBackoff(t *testing.T) {
	worker := &SendWorker{MaxAttempts: 3}
	before := time.Now()
	got := worker.NextRetry(newJob(2))
	delta := got.Sub(before)
	if delta < time.Second-50*time.Millisecond || delta > time.Second+250*time.Millisecond {
		t.Fatalf("NextRetry(attempt 2) delta = %v, want ~1s", delta)
	}
}

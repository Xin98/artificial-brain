package river

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	riverqueue "github.com/riverqueue/river"
	riverpgxv5 "github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/inbound/worker"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/outbound/postgres"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url, ok := os.LookupEnv("TEST_DATABASE_URL")
	if !ok {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	directory := filepath.Join("..", "..", "..", "..", "..", "..", "..", "deploy", "migrations")
	if err := database.RunMigrations(ctx, url, directory); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	pool, err := database.OpenPool(ctx, url)
	if err != nil {
		t.Fatalf("OpenPool() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `truncate table reminder.reminder_plans, reminder.reminder_deliveries restart identity cascade`); err != nil {
		t.Fatalf("truncate reminder tables error = %v", err)
	}
	// river_client/river_client_queue are created and later dropped again by
	// migration 006 (River v0.44.0 removed them), so only the live tables are
	// truncated here.
	if _, err := pool.Exec(ctx, `truncate table river_job, river_queue, river_leader, river_notification restart identity cascade`); err != nil {
		t.Fatalf("truncate river tables error = %v", err)
	}
	return pool
}

func randomID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read() error = %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// newInsertClient builds the insert-only River client the scheduler adapter is
// constructed with in production wiring.
func newInsertClient(t *testing.T, pool *pgxpool.Pool) *riverqueue.Client[pgx.Tx] {
	t.Helper()
	client, err := riverqueue.NewClient(riverpgxv5.New(pool), &riverqueue.Config{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

// startWorkerClient starts a River worker client serving reminder_email and
// reminder_sms with one worker each, running the reminder SendWorker over the
// provided handler. The client is stopped during test cleanup.
func startWorkerClient(t *testing.T, pool *pgxpool.Pool, handler *command.SendReminderHandler, maxAttempts int, configure func(*riverqueue.Config)) *riverqueue.Client[pgx.Tx] {
	t.Helper()
	workers := riverqueue.NewWorkers()
	riverqueue.AddWorker(workers, &worker.SendWorker{Handler: handler, MaxAttempts: maxAttempts})
	config := &riverqueue.Config{
		Queues: map[string]riverqueue.QueueConfig{
			"reminder_email": {MaxWorkers: 1},
			"reminder_sms":   {MaxWorkers: 1},
		},
		Workers:           workers,
		FetchCooldown:     50 * time.Millisecond,
		FetchPollInterval: 100 * time.Millisecond,
	}
	if configure != nil {
		configure(config)
	}
	client, err := riverqueue.NewClient(riverpgxv5.New(pool), config)
	if err != nil {
		t.Fatalf("NewClient(worker) error = %v", err)
	}
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.StopAndCancel(stopCtx)
		<-client.Stopped()
	})
	return client
}

// stubTodoReader re-reads every todo as the configured pending todo.
type stubTodoReader struct {
	view dto.TodoView
}

func (r *stubTodoReader) Get(context.Context, string, string, string) (dto.TodoView, error) {
	return r.view, nil
}

// stubChannelResolver resolves every channel to one usable endpoint.
type stubChannelResolver struct {
	endpoint dto.ChannelEndpoint
}

func (r stubChannelResolver) Resolve(context.Context, string, string, string) (dto.ChannelEndpoint, error) {
	return r.endpoint, nil
}

// recordingNotifier counts every send attempt and success. failFirst fails
// the first attempt with a transient error; sleepFirst makes the first attempt
// sleep (respecting context cancellation) to simulate a slow provider call.
type recordingNotifier struct {
	mu         sync.Mutex
	attempts   int
	successes  int
	messages   []dto.ReminderMessage
	failFirst  bool
	sleepFirst time.Duration
}

func (n *recordingNotifier) Send(ctx context.Context, message dto.ReminderMessage) (dto.SendResult, error) {
	n.mu.Lock()
	n.attempts++
	attempt := n.attempts
	n.mu.Unlock()

	if n.sleepFirst > 0 && attempt == 1 {
		select {
		case <-time.After(n.sleepFirst):
		case <-ctx.Done():
			return dto.SendResult{}, ctx.Err()
		}
	}
	if n.failFirst && attempt == 1 {
		return dto.SendResult{}, errors.New("transient provider failure")
	}

	n.mu.Lock()
	n.successes++
	n.messages = append(n.messages, message)
	n.mu.Unlock()
	return dto.SendResult{ProviderMessageID: fmt.Sprintf("provider-message-%d", attempt)}, nil
}

func (n *recordingNotifier) counts() (attempts, successes int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.attempts, n.successes
}

// newTestHandler wires the real SendReminderHandler over the real postgres
// plan/delivery stores and recording fakes for everything provider-facing.
func newTestHandler(t *testing.T, pool *pgxpool.Pool, ownerUserID string, reminderVersion int) (*command.SendReminderHandler, *recordingNotifier, *recordingNotifier) {
	t.Helper()
	email := &recordingNotifier{}
	sms := &recordingNotifier{}
	handler := &command.SendReminderHandler{
		Plans:      postgres.NewPlanStore(pool),
		Deliveries: postgres.NewDeliveryStore(pool),
		Todos: &stubTodoReader{view: dto.TodoView{
			Title:           "写周报",
			Status:          "pending",
			ReminderVersion: reminderVersion,
			OwnerUserID:     ownerUserID,
		}},
		Channels: stubChannelResolver{endpoint: dto.ChannelEndpoint{Address: "user@example.com", Usable: true}},
		Email:    email,
		Sms:      sms,
		Log:      slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})),
		Now:      func() time.Time { return time.Now().UTC() },
		NewID:    func() string { return randomID(t) },
	}
	return handler, email, sms
}

// seedPlanAndDelivery stores a plan and its scheduled delivery row the way the
// planner does, so the worker finds the auditable row at execution time.
func seedPlanAndDelivery(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID, ownerUserID, todoID string, reminderVersion int, scheduledAt time.Time, channel string) (domain.ReminderPlan, domain.ReminderDelivery) {
	t.Helper()
	plan, err := domain.NewReminderPlan(randomID(t), workspaceID, todoID, reminderVersion, scheduledAt, []string{channel}, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewReminderPlan() error = %v", err)
	}
	if err := postgres.NewPlanStore(pool).Save(ctx, plan); err != nil {
		t.Fatalf("PlanStore.Save() error = %v", err)
	}
	delivery, err := domain.NewDelivery(randomID(t), workspaceID, ownerUserID, todoID, reminderVersion, plan.ID, channel, "写周报", scheduledAt, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewDelivery() error = %v", err)
	}
	if err := postgres.NewDeliveryStore(pool).Save(ctx, delivery); err != nil {
		t.Fatalf("DeliveryStore.Save() error = %v", err)
	}
	return plan, delivery
}

// scheduleCommitted schedules the reminder job inside a committed TxRunner
// transaction, writing the job ID back onto the delivery row, and returns the
// job ID.
func scheduleCommitted(t *testing.T, pool *pgxpool.Pool, scheduler ports.JobScheduler, deliveries ports.DeliveryStore, job ports.ReminderJob, delivery domain.ReminderDelivery) int64 {
	t.Helper()
	var jobID int64
	err := database.NewTxRunner(pool).Run(context.Background(), func(txCtx context.Context) error {
		scheduled, err := scheduler.Schedule(txCtx, job)
		if err != nil {
			return err
		}
		if len(scheduled) != 1 {
			return fmt.Errorf("Schedule() returned %d channels, want 1", len(scheduled))
		}
		jobID = scheduled[0].JobID
		return deliveries.SetProviderJobID(txCtx, job.WorkspaceID, delivery.ID, jobID)
	})
	if err != nil {
		t.Fatalf("TxRunner.Run(schedule) error = %v", err)
	}
	return jobID
}

func waitFor(t *testing.T, timeout time.Duration, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

func reloadDelivery(t *testing.T, pool *pgxpool.Pool, workspaceID, idempotencyKey string) domain.ReminderDelivery {
	t.Helper()
	delivery, err := postgres.NewDeliveryStore(pool).ByIdempotencyKey(context.Background(), workspaceID, idempotencyKey)
	if err != nil {
		t.Fatalf("ByIdempotencyKey() error = %v", err)
	}
	return delivery
}

// TestSchedulerScheduleIsAtomicWithAmbientTransaction proves InsertTx joins the
// caller's transaction: the job row is visible inside the tx, rolls back with
// it, and commits with it.
func TestSchedulerScheduleIsAtomicWithAmbientTransaction(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	scheduler := New(newInsertClient(t, pool))
	runner := database.NewTxRunner(pool)
	scheduledAt := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	job := ports.ReminderJob{
		PlanID:              randomID(t),
		WorkspaceID:         randomID(t),
		OwnerUserID:         randomID(t),
		TodoID:              randomID(t),
		TodoReminderVersion: 1,
		ScheduledAtUTC:      scheduledAt,
		Channels:            []string{"email"},
	}

	// Rollback: the job row is visible inside the tx, then disappears with it.
	var jobID int64
	rollbackErr := errors.New("roll back")
	err := runner.Run(ctx, func(txCtx context.Context) error {
		scheduled, err := scheduler.Schedule(txCtx, job)
		if err != nil {
			return err
		}
		if len(scheduled) != 1 || scheduled[0].Channel != "email" || scheduled[0].JobID == 0 {
			return fmt.Errorf("Schedule() = %#v, want one email channel with a job ID", scheduled)
		}
		jobID = scheduled[0].JobID
		var count int
		err = database.ExecutorFromContextOr(txCtx, pool).QueryRow(txCtx,
			`select count(*) from river_job where id = $1`, jobID).Scan(&count)
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("river_job row visible inside tx = %d, want 1", count)
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("Run() error = %v, want rollbackErr", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from river_job where id = $1`, jobID).Scan(&count); err != nil {
		t.Fatalf("count after rollback error = %v", err)
	}
	if count != 0 {
		t.Fatalf("river_job rows after rollback = %d, want 0", count)
	}

	// Commit: the row lands with the planned queue and schedule.
	if err := runner.Run(ctx, func(txCtx context.Context) error {
		scheduled, err := scheduler.Schedule(txCtx, job)
		if err != nil {
			return err
		}
		jobID = scheduled[0].JobID
		return nil
	}); err != nil {
		t.Fatalf("Run(commit) error = %v", err)
	}
	var queue, state string
	var gotScheduledAt time.Time
	err = pool.QueryRow(ctx, `select queue, state, scheduled_at from river_job where id = $1`, jobID).
		Scan(&queue, &state, &gotScheduledAt)
	if err != nil {
		t.Fatalf("select committed job error = %v", err)
	}
	if queue != "reminder_email" {
		t.Fatalf("queue = %q, want reminder_email", queue)
	}
	if state != "scheduled" {
		t.Fatalf("state = %q, want scheduled", state)
	}
	if !gotScheduledAt.UTC().Equal(scheduledAt) {
		t.Fatalf("scheduled_at = %v, want %v", gotScheduledAt, scheduledAt)
	}
}

// TestSchedulerCancelMarksJobCancelled commits a scheduled job, cancels it,
// and verifies the river_job row ends cancelled.
func TestSchedulerCancelMarksJobCancelled(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	scheduler := New(newInsertClient(t, pool))
	workspaceID, ownerUserID, todoID := randomID(t), randomID(t), randomID(t)
	scheduledAt := time.Now().UTC().Add(time.Hour)
	plan, delivery := seedPlanAndDelivery(t, ctx, pool, workspaceID, ownerUserID, todoID, 1, scheduledAt, "email")
	jobID := scheduleCommitted(t, pool, scheduler, postgres.NewDeliveryStore(pool), ports.ReminderJob{
		PlanID:              plan.ID,
		WorkspaceID:         workspaceID,
		OwnerUserID:         ownerUserID,
		TodoID:              todoID,
		TodoReminderVersion: 1,
		ScheduledAtUTC:      scheduledAt,
		Channels:            []string{"email"},
	}, delivery)

	if err := scheduler.Cancel(ctx, jobID); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	var state string
	if err := pool.QueryRow(ctx, `select state from river_job where id = $1`, jobID).Scan(&state); err != nil {
		t.Fatalf("select state error = %v", err)
	}
	if state != "cancelled" {
		t.Fatalf("state after cancel = %q, want cancelled", state)
	}

	// Errors pass through: cancelling an unknown job surfaces River's not-found.
	if err := scheduler.Cancel(ctx, 999999999); !errors.Is(err, rivertype.ErrNotFound) {
		t.Fatalf("Cancel(unknown) error = %v, want rivertype.ErrNotFound", err)
	}
}

// TestScheduleAndWorkDeliversReminderEndToEnd schedules a reminder one second
// out, runs a real River worker client against the real postgres stores and
// recording notifier fakes, and waits for the delivery to succeed.
func TestScheduleAndWorkDeliversReminderEndToEnd(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	scheduler := New(newInsertClient(t, pool))
	workspaceID, ownerUserID, todoID := randomID(t), randomID(t), randomID(t)
	scheduledAt := time.Now().UTC().Add(time.Second)
	plan, delivery := seedPlanAndDelivery(t, ctx, pool, workspaceID, ownerUserID, todoID, 1, scheduledAt, "email")
	handler, email, sms := newTestHandler(t, pool, ownerUserID, 1)
	scheduleCommitted(t, pool, scheduler, postgres.NewDeliveryStore(pool), ports.ReminderJob{
		PlanID:              plan.ID,
		WorkspaceID:         workspaceID,
		OwnerUserID:         ownerUserID,
		TodoID:              todoID,
		TodoReminderVersion: 1,
		ScheduledAtUTC:      scheduledAt,
		Channels:            []string{"email"},
	}, delivery)

	startWorkerClient(t, pool, handler, 5, nil)

	waitFor(t, 15*time.Second, "the email delivery to succeed", func() bool {
		_, successes := email.counts()
		return successes == 1
	})
	got := reloadDelivery(t, pool, workspaceID, delivery.IdempotencyKey)
	if got.State != domain.StateSucceeded {
		t.Fatalf("delivery state = %q, want succeeded", got.State)
	}
	if got.ProviderMessageID == nil || *got.ProviderMessageID == "" {
		t.Fatalf("delivery provider message ID = %v, want set", got.ProviderMessageID)
	}
	if attempts, _ := sms.counts(); attempts != 0 {
		t.Fatalf("sms attempts = %d, want 0", attempts)
	}
}

// TestDuplicateExecutionSendsOnce works a job to success, then invokes the
// handler a second time for the same args: the delivery is final, so the
// notifier is never called again.
func TestDuplicateExecutionSendsOnce(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	scheduler := New(newInsertClient(t, pool))
	workspaceID, ownerUserID, todoID := randomID(t), randomID(t), randomID(t)
	scheduledAt := time.Now().UTC()
	plan, delivery := seedPlanAndDelivery(t, ctx, pool, workspaceID, ownerUserID, todoID, 1, scheduledAt, "email")
	handler, email, _ := newTestHandler(t, pool, ownerUserID, 1)
	scheduleCommitted(t, pool, scheduler, postgres.NewDeliveryStore(pool), ports.ReminderJob{
		PlanID:              plan.ID,
		WorkspaceID:         workspaceID,
		OwnerUserID:         ownerUserID,
		TodoID:              todoID,
		TodoReminderVersion: 1,
		ScheduledAtUTC:      scheduledAt,
		Channels:            []string{"email"},
	}, delivery)

	startWorkerClient(t, pool, handler, 5, nil)
	waitFor(t, 15*time.Second, "the email delivery to succeed", func() bool {
		_, successes := email.counts()
		return successes == 1
	})

	// A duplicate execution of the same delivery must not send again.
	err := handler.Handle(ctx, command.SendRequest{
		WorkspaceID:  workspaceID,
		OwnerUserID:  ownerUserID,
		PlanID:       plan.ID,
		Channel:      "email",
		FinalAttempt: true,
	})
	if err != nil {
		t.Fatalf("duplicate Handle() error = %v", err)
	}
	attempts, successes := email.counts()
	if attempts != 1 || successes != 1 {
		t.Fatalf("notifier counts after duplicate = (attempts %d, successes %d), want (1, 1)", attempts, successes)
	}
}

// TestTransientFailureIsRetriedUntilSuccess fails the first notifier call with
// a transient error and verifies the queue retries the job to success with
// exactly two delivery attempts.
func TestTransientFailureIsRetriedUntilSuccess(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	scheduler := New(newInsertClient(t, pool))
	workspaceID, ownerUserID, todoID := randomID(t), randomID(t), randomID(t)
	scheduledAt := time.Now().UTC()
	plan, delivery := seedPlanAndDelivery(t, ctx, pool, workspaceID, ownerUserID, todoID, 1, scheduledAt, "email")
	handler, email, _ := newTestHandler(t, pool, ownerUserID, 1)
	email.failFirst = true
	scheduleCommitted(t, pool, scheduler, postgres.NewDeliveryStore(pool), ports.ReminderJob{
		PlanID:              plan.ID,
		WorkspaceID:         workspaceID,
		OwnerUserID:         ownerUserID,
		TodoID:              todoID,
		TodoReminderVersion: 1,
		ScheduledAtUTC:      scheduledAt,
		Channels:            []string{"email"},
	}, delivery)

	startWorkerClient(t, pool, handler, 5, nil)

	waitFor(t, 15*time.Second, "the retried delivery to succeed", func() bool {
		_, successes := email.counts()
		return successes == 1
	})
	attempts, successes := email.counts()
	if attempts != 2 || successes != 1 {
		t.Fatalf("notifier counts = (attempts %d, successes %d), want (2, 1)", attempts, successes)
	}
	got := reloadDelivery(t, pool, workspaceID, delivery.IdempotencyKey)
	if got.State != domain.StateSucceeded {
		t.Fatalf("delivery state = %q, want succeeded", got.State)
	}
	if got.AttemptCount != 2 {
		t.Fatalf("delivery attempt count = %d, want 2", got.AttemptCount)
	}
}

// TestCrashMidFlightDeliversExactlyOnce simulates a worker crash mid-flight:
// the provider call sleeps 2s, the first client is stopped with a ~500ms
// budget while the job is running, and a replacement client must deliver
// exactly one success.
func TestCrashMidFlightDeliversExactlyOnce(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	scheduler := New(newInsertClient(t, pool))
	workspaceID, ownerUserID, todoID := randomID(t), randomID(t), randomID(t)
	scheduledAt := time.Now().UTC()
	plan, delivery := seedPlanAndDelivery(t, ctx, pool, workspaceID, ownerUserID, todoID, 1, scheduledAt, "email")
	handler, email, _ := newTestHandler(t, pool, ownerUserID, 1)
	email.sleepFirst = 2 * time.Second
	scheduleCommitted(t, pool, scheduler, postgres.NewDeliveryStore(pool), ports.ReminderJob{
		PlanID:              plan.ID,
		WorkspaceID:         workspaceID,
		OwnerUserID:         ownerUserID,
		TodoID:              todoID,
		TodoReminderVersion: 1,
		ScheduledAtUTC:      scheduledAt,
		Channels:            []string{"email"},
	}, delivery)

	client := startWorkerClient(t, pool, handler, 5, nil)

	waitFor(t, 10*time.Second, "the first attempt to start", func() bool {
		attempts, _ := email.counts()
		return attempts >= 1
	})

	// Stop mid-flight with a ~500ms budget, standing in for a crash: the
	// in-flight job cannot complete normally.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	_ = client.StopAndCancel(stopCtx)
	stopCancel()
	waitFor(t, 10*time.Second, "the first client to stop", func() bool {
		select {
		case <-client.Stopped():
			return true
		default:
			return false
		}
	})

	// A replacement client (fresh process) takes over. Tight job timeout and
	// rescue window let it also recover a job left stuck in running state.
	startWorkerClient(t, pool, handler, 5, func(config *riverqueue.Config) {
		config.JobTimeout = time.Second
		config.RescueStuckJobsAfter = time.Second
	})

	waitFor(t, 20*time.Second, "exactly one successful delivery", func() bool {
		_, successes := email.counts()
		if successes != 1 {
			return false
		}
		got, err := postgres.NewDeliveryStore(pool).ByIdempotencyKey(ctx, workspaceID, delivery.IdempotencyKey)
		return err == nil && got.State == domain.StateSucceeded
	})
	attempts, successes := email.counts()
	if successes != 1 {
		t.Fatalf("successful sends = %d (attempts %d), want exactly 1", successes, attempts)
	}
	got := reloadDelivery(t, pool, workspaceID, delivery.IdempotencyKey)
	if got.State != domain.StateSucceeded {
		t.Fatalf("delivery state = %q, want succeeded", got.State)
	}
	if got.AttemptCount != 2 {
		t.Fatalf("delivery attempt count = %d, want 2", got.AttemptCount)
	}
}

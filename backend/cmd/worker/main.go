package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	riverqueue "github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	identitypostgres "github.com/Xin98/artificial-brain/backend/internal/modules/identity/adapters/outbound/postgres"
	reminderworker "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/inbound/worker"
	aliyunprovider "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/outbound/aliyun"
	fakeprovider "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/outbound/fake"
	reminderpostgres "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/outbound/postgres"
	smtpadapter "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/outbound/smtp"
	remindercommand "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/command"
	reminderdto "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	reminderports "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	todopostgres "github.com/Xin98/artificial-brain/backend/internal/modules/todo/adapters/outbound/postgres"
	tododomain "github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/config"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/Xin98/artificial-brain/backend/internal/platform/observability"
	"github.com/Xin98/artificial-brain/backend/internal/platform/server"
	"github.com/Xin98/artificial-brain/backend/internal/platform/workerstatus"
)

func main() {
	os.Exit(run())
}

func run() int {
	logger := observability.NewLogger(os.Stderr, "worker", "unknown")
	cfg, err := config.Load(config.RoleWorker, os.LookupEnv)
	if err != nil {
		// The runbook depends on operators seeing WHICH variable is missing
		// or invalid, so the config error must reach the log line.
		logger.Error("invalid worker configuration", "error", err)
		return 1
	}
	logger = observability.NewLogger(os.Stderr, cfg.ServiceName, cfg.ServiceVersion)

	pool, err := database.OpenPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("database startup failed")
		return 1
	}
	defer pool.Close()

	if err := database.RequireSchema(context.Background(), pool, database.CurrentSchemaVersion); err != nil {
		logger.Error("database schema verification failed")
		return 1
	}

	instanceID, err := workerInstanceID()
	if err != nil {
		logger.Error("worker instance ID generation failed")
		return 1
	}
	ticks, err := workerstatus.NewTimeTickSource(cfg.HeartbeatInterval)
	if err != nil {
		logger.Error("worker heartbeat configuration failed")
		return 1
	}

	registry := workerstatus.NewRegistry(pool, time.Now)
	state := &workerstatus.State{}
	heartbeat := workerstatus.NewHeartbeat(stateRecorder{Recorder: registry, state: state}, workerstatus.Instance{
		ID:        instanceID,
		Version:   cfg.ServiceVersion,
		StartedAt: time.Now(),
	}, ticks)

	riverClient, err := buildRiverClient(cfg, pool, logger)
	if err != nil {
		logger.Error("river client construction failed")
		return 1
	}
	var riverStarted atomic.Bool
	ready := func(ctx context.Context) error {
		if !riverStarted.Load() {
			return errors.New("river client has not started")
		}
		if err := pool.Ping(ctx); err != nil {
			return err
		}
		return database.RequireSchema(ctx, pool, database.CurrentSchemaVersion)
	}
	healthServer := &http.Server{Addr: cfg.HTTPAddress, Handler: server.NewWorkerHealthHandler(ready, state.Ready)}

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()

	errs := make(chan error, 3)
	go func() { errs <- heartbeat.Run(ctx) }()
	go func() { errs <- server.Serve(ctx, healthServer, cfg.ShutdownTimeout) }()
	go func() { errs <- runRiverClient(ctx, riverClient, &riverStarted, cfg.ShutdownTimeout) }()

	var failed bool
	for range 3 {
		err := <-errs
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
			failed = true
			cancel()
		}
	}
	if failed {
		logger.Error("worker runtime failed")
		return 1
	}
	return 0
}

// reminderQueues selects the delivery queues this worker serves: the SMS
// queue disappears when the SMS adapter is disabled, so SMS jobs are never
// worked into a provider that does not exist.
func reminderQueues(cfg config.Config) map[string]riverqueue.QueueConfig {
	queues := map[string]riverqueue.QueueConfig{
		"reminder_email": {MaxWorkers: cfg.ReminderQueueEmailConcurrency},
	}
	if cfg.ReminderSmsAdapter != config.ReminderSmsAdapterDisabled {
		queues["reminder_sms"] = riverqueue.QueueConfig{MaxWorkers: cfg.ReminderQueueSmsConcurrency}
	}
	return queues
}

// buildRiverClient composes the reminder delivery command and registers its
// River worker on the reminder_email and reminder_sms queues.
func buildRiverClient(cfg config.Config, pool *pgxpool.Pool, logger *slog.Logger) (*riverqueue.Client[pgx.Tx], error) {
	workers := riverqueue.NewWorkers()
	riverqueue.AddWorker(workers, &reminderworker.SendWorker{
		Handler:     buildSendReminderHandler(cfg, pool, logger),
		MaxAttempts: cfg.ReminderJobMaxAttempts,
	})
	return riverqueue.NewClient(riverpgxv5.New(pool), &riverqueue.Config{
		Workers: workers,
		Queues:  reminderQueues(cfg),
	})
}

// runRiverClient starts the River worker client and stops it gracefully once
// ctx is cancelled; a start failure is returned so the worker process fails.
func runRiverClient(ctx context.Context, client *riverqueue.Client[pgx.Tx], started *atomic.Bool, shutdownTimeout time.Duration) error {
	if err := client.Start(ctx); err != nil {
		return err
	}
	started.Store(true)
	<-ctx.Done()
	stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := client.Stop(stopCtx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// buildSendReminderHandler composes the reminder delivery command from the
// real postgres stores, the cmd-local todo/channel shims, and the provider
// notifiers selected by configuration.
func buildSendReminderHandler(cfg config.Config, pool *pgxpool.Pool, logger *slog.Logger) *remindercommand.SendReminderHandler {
	return &remindercommand.SendReminderHandler{
		Plans:      reminderpostgres.NewPlanStore(pool),
		Deliveries: reminderpostgres.NewDeliveryStore(pool),
		Todos:      todoReaderShim{store: todopostgres.NewStore(pool)},
		Channels:   channelResolverShim{channels: identitypostgres.NewChannelStore(pool)},
		Email:      selectEmailNotifier(cfg, pool),
		Sms:        selectSmsNotifier(cfg, pool),
		Log:        logger,
		NewID:      newID,
		Now:        time.Now,
	}
}

// selectEmailNotifier resolves the configured email provider; config.Load
// already validated the choice, so the fallback is the fake adapter.
func selectEmailNotifier(cfg config.Config, pool *pgxpool.Pool) reminderports.EmailNotifier {
	if cfg.ReminderEmailAdapter == config.ReminderEmailAdapterSmtp {
		return smtpadapter.New(smtpadapter.Config{
			Host:     cfg.ReminderSmtpHost,
			Port:     cfg.ReminderSmtpPort,
			Username: cfg.ReminderSmtpUsername,
			Password: cfg.ReminderSmtpPassword,
			From:     cfg.ReminderSmtpFrom,
			Timeout:  cfg.ReminderSmtpTimeout,
		})
	}
	outbox := fakeprovider.NewOutbox(pool)
	return fakeprovider.NewEmail(outbox)
}

// selectSmsNotifier resolves the configured SMS provider; config.Load already
// validated the choice, so the fallback is the fake adapter.
func selectSmsNotifier(cfg config.Config, pool *pgxpool.Pool) reminderports.SmsNotifier {
	if cfg.ReminderSmsAdapter == config.ReminderSmsAdapterDisabled {
		return disabledSmsNotifier{}
	}
	if cfg.ReminderSmsAdapter == config.ReminderSmsAdapterAliyun {
		return aliyunprovider.New(aliyunprovider.Config{
			Endpoint:        cfg.ReminderAliyunEndpoint,
			AccessKeyID:     cfg.ReminderAliyunAccessKeyID,
			AccessKeySecret: cfg.ReminderAliyunAccessKeySecret,
			SignName:        cfg.ReminderAliyunSignName,
			TemplateCode:    cfg.ReminderAliyunTemplateCode,
		})
	}
	outbox := fakeprovider.NewOutbox(pool)
	return fakeprovider.NewSms(outbox)
}

// disabledSmsNotifier fails closed if an SMS delivery job ever executes while
// the adapter is disabled. channelsProvider never plans SMS in this state, so
// reaching this path means the job predates the switch; it dead-letters after
// its attempts exhaust instead of touching a nonexistent provider.
type disabledSmsNotifier struct{}

func (disabledSmsNotifier) Send(_ context.Context, _ reminderdto.ReminderMessage) (reminderdto.SendResult, error) {
	return reminderdto.SendResult{}, errors.New("reminder: sms adapter is disabled")
}

// todoReaderShim adapts Todo's postgres store to Reminder's TodoReader port.
// The todo module's not-found sentinel maps to the reminder port's own
// sentinel so the reminder application layer never imports Todo.
type todoReaderShim struct {
	store *todopostgres.Store
}

func (s todoReaderShim) Get(ctx context.Context, workspaceID, ownerUserID, todoID string) (reminderdto.TodoView, error) {
	todo, err := s.store.Get(ctx, workspaceID, ownerUserID, todoID)
	if errors.Is(err, tododomain.ErrTodoNotFound) {
		return reminderdto.TodoView{}, reminderports.ErrTodoNotFound
	}
	if err != nil {
		return reminderdto.TodoView{}, err
	}
	return reminderdto.TodoView{
		Title:           todo.Title,
		Status:          string(todo.Status),
		ReminderVersion: todo.ReminderVersion,
		OwnerUserID:     todo.OwnerUserID,
	}, nil
}

// channelResolverShim adapts Identity's ChannelStore to Reminder's
// ChannelResolver port: resolution stays workspace+user scoped, and a missing
// channel resolves to a zero, unusable endpoint (nil error) so the send
// command suppresses instead of failing.
type channelResolverShim struct {
	channels *identitypostgres.ChannelStore
}

func (s channelResolverShim) Resolve(ctx context.Context, workspaceID, userID, channel string) (reminderdto.ChannelEndpoint, error) {
	rows, err := s.channels.ListByUser(ctx, workspaceID, userID)
	if err != nil {
		return reminderdto.ChannelEndpoint{}, err
	}
	for _, row := range rows {
		if string(row.Kind) == channel {
			return reminderdto.ChannelEndpoint{Address: row.Address, Usable: row.Usable()}, nil
		}
	}
	return reminderdto.ChannelEndpoint{}, nil
}

// newID returns a random RFC 4122 version 4 UUID string.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type stateRecorder struct {
	workerstatus.Recorder
	state *workerstatus.State
}

func (r stateRecorder) Record(ctx context.Context, instance workerstatus.Instance) error {
	err := r.Recorder.Record(ctx, instance)
	if err != nil {
		r.state.MarkHeartbeatFailure()
		return err
	}
	r.state.MarkHeartbeatSuccess()
	return nil
}

func workerInstanceID() (string, error) {
	if id := os.Getenv("WORKER_INSTANCE_ID"); id != "" {
		return id, nil
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}

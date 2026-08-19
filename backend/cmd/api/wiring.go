package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	conversationhttp "github.com/Xin98/artificial-brain/backend/internal/modules/conversation/adapters/inbound/http"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/adapters/outbound/deterministic"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/adapters/outbound/openai"
	convpostgres "github.com/Xin98/artificial-brain/backend/internal/modules/conversation/adapters/outbound/postgres"
	convapplication "github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application"
	convcommand "github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/command"
	convports "github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	conversationdomain "github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
	identityhttp "github.com/Xin98/artificial-brain/backend/internal/modules/identity/adapters/inbound/http"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/adapters/outbound/fakeoutbox"
	identitypostgres "github.com/Xin98/artificial-brain/backend/internal/modules/identity/adapters/outbound/postgres"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/query"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/outbound/noopjob"
	reminderpostgres "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/outbound/postgres"
	remindercommand "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/command"
	reminderdto "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	reminderports "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	reminderdomain "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
	todohttp "github.com/Xin98/artificial-brain/backend/internal/modules/todo/adapters/inbound/http"
	todopostgres "github.com/Xin98/artificial-brain/backend/internal/modules/todo/adapters/outbound/postgres"
	todocommand "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/command"
	tododto "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	todoports "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/ports"
	todoquery "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/query"
	tododomain "github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/config"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/Xin98/artificial-brain/backend/internal/platform/server"
	"github.com/Xin98/artificial-brain/backend/internal/platform/systemhealth"
)

// buildHandler composes the API handler: health routes, identity and todo
// routes behind the session middleware, and the gated dev inbox. It is the
// single composition seam so the wiring is exercised by the composition
// integration test.
func buildHandler(cfg config.Config, pool *pgxpool.Pool, ready server.Readiness, checker *systemhealth.Checker) http.Handler {
	mux := http.NewServeMux()
	server.RegisterHealthRoutes(mux, ready, checker)
	auth := newAuthMiddleware(pool)
	registerIdentityRoutes(cfg, pool, mux, auth)
	todos := buildTodoHandlers(pool, noopjob.New(), time.Now)
	registerTodoRoutes(mux, auth, todos)
	registerConversationRoutes(cfg, pool, mux, auth, todos)
	if cfg.DevInboxEnabled && cfg.AppEnv != config.AppEnvProduction {
		mux.Handle("GET /api/v1/dev/sms-inbox", identityhttp.NewDevInboxHandler(identitypostgres.NewOutboxReader(pool)))
	}
	return server.NewAPIRouter(mux)
}

// newAuthMiddleware builds Identity's session middleware once so every
// protected route shares one authenticator.
func newAuthMiddleware(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	sessions := identitypostgres.NewSessionStore(pool)
	authenticator := (&query.SessionQuery{Sessions: sessions, Now: time.Now}).Authenticate
	return identityhttp.NewAuthMiddleware(authenticator)
}

func registerIdentityRoutes(cfg config.Config, pool *pgxpool.Pool, mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	users := identitypostgres.NewUserStore(pool)
	workspaces := identitypostgres.NewWorkspaceStore(pool)
	challenges := identitypostgres.NewChallengeStore(pool)
	sessions := identitypostgres.NewSessionStore(pool)
	channels := identitypostgres.NewChannelStore(pool)
	outbox := fakeoutbox.New(pool)

	handler := &identityhttp.Handler{
		RequestLoginChallenge: &command.RequestLoginChallengeHandler{
			Challenges:   challenges,
			Outbox:       outbox,
			NewCode:      newSixDigitCode,
			NewID:        newID,
			Now:          time.Now,
			ChallengeTTL: cfg.LoginChallengeTTL,
		},
		VerifyLoginChallenge: &command.VerifyLoginChallengeHandler{
			Challenges: challenges,
			Users:      users,
			Workspaces: workspaces,
			Sessions:   sessions,
			NewID:      newID,
			NewToken:   newSessionToken,
			Now:        time.Now,
			SessionTTL: cfg.SessionTTL,
		},
		Logout: &command.LogoutHandler{Sessions: sessions, Now: time.Now},
		AddChannel: &command.AddChannelHandler{
			Channels: channels,
			Outbox:   outbox,
			NewCode:  newSixDigitCode,
			NewID:    newID,
			Now:      time.Now,
			CodeTTL:  cfg.ChannelCodeTTL,
		},
		VerifyChannel:     &command.VerifyChannelHandler{Channels: channels, Now: time.Now},
		SetChannelEnabled: &command.SetChannelEnabledHandler{Channels: channels},
		Channels:          &query.ChannelsQuery{Channels: channels},
		SessionTTL:        cfg.SessionTTL,
	}

	identityhttp.RegisterRoutes(mux, auth, handler)
}

// newID returns a random RFC 4122 version 4 UUID string.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// newSixDigitCode returns a cryptographically random six-digit login code.
func newSixDigitCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// newSessionToken returns a 32-byte hex bearer token.
func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// todoHandlers groups the Todo command handlers wired against the platform
// transaction runner and the Reminder seam.
type todoHandlers struct {
	Store    todoports.TodoStore
	Create   *todocommand.CreateTodoHandler
	Complete *todocommand.CompleteTodoHandler
	Delete   *todocommand.DeleteTodoHandler
	Update   *todocommand.UpdateTodoHandler
}

// buildTodoHandlers wires the Todo commands. The scheduler is a parameter so
// the composition test can prove atomic rollback with a failing fake; the
// channel snapshot stays empty until delivery lands in ITER-0003 (A5).
func buildTodoHandlers(pool *pgxpool.Pool, scheduler reminderports.JobScheduler, now func() time.Time) todoHandlers {
	uow := &joinableUoW{runner: database.NewTxRunner(pool)}
	todos := todopostgres.NewStore(pool)
	plans := reminderpostgres.NewPlanStore(pool)
	// INTERIM: Task 12 replaces noopDeliveryStore with the real postgres
	// DeliveryStore (Task 7) once it exists.
	deliveries := noopDeliveryStore{}
	planner := &reminderPlannerShim{
		plan:   &remindercommand.PlanReminderHandler{Plans: plans, Deliveries: deliveries, Scheduler: scheduler, NewID: newID, Now: now},
		revoke: &remindercommand.RevokePlansHandler{Plans: plans, Deliveries: deliveries, Scheduler: scheduler, Log: slog.Default(), Now: now},
	}
	return todoHandlers{
		Store:    todos,
		Create:   &todocommand.CreateTodoHandler{Store: todos, UoW: uow, Planner: planner, NewID: newID, Now: now},
		Complete: &todocommand.CompleteTodoHandler{Store: todos, UoW: uow, Planner: planner, Now: now},
		Delete:   &todocommand.DeleteTodoHandler{Store: todos, UoW: uow, Planner: planner, Now: now},
		Update:   &todocommand.UpdateTodoHandler{Store: todos, UoW: uow, Planner: planner, Now: now},
	}
}

// joinableUoW runs work in the ambient transaction when one exists and
// otherwise starts a fresh one (D11). It lets Conversation's unit of work
// compose Todo's public handlers into exactly one real transaction while the
// platform TxRunner keeps rejecting direct nesting.
type joinableUoW struct {
	runner *database.TxRunner
}

func (u *joinableUoW) Run(ctx context.Context, work func(context.Context) error) error {
	if _, ok := database.ExecutorFromContext(ctx); ok {
		return work(ctx)
	}
	return u.runner.Run(ctx, work)
}

// registerTodoRoutes exposes the todo and dashboard routes behind the auth
// middleware.
func registerTodoRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler, handlers todoHandlers) {
	todohttp.RegisterRoutes(mux, auth, &todohttp.Handler{
		Create:    handlers.Create,
		Complete:  handlers.Complete,
		Update:    handlers.Update,
		List:      &todoquery.ListTodosHandler{Store: handlers.Store, Now: time.Now},
		Get:       &todoquery.GetTodoHandler{Store: handlers.Store, Now: time.Now},
		Dashboard: &todoquery.DashboardSummaryHandler{Store: handlers.Store, Now: time.Now},
	})
}

// todoGatewayShim adapts Todo's public application handlers to the
// Conversation-owned TodoGateway port (D6), translating Todo's outcomes into
// Conversation-owned errors so Conversation never imports Todo's domain.
type todoGatewayShim struct {
	create     *todocommand.CreateTodoHandler
	del        *todocommand.DeleteTodoHandler
	list       *todoquery.ListTodosHandler
	get        *todoquery.GetTodoHandler
	candidates *todoquery.SearchCandidatesHandler
}

func (s *todoGatewayShim) CreateTodo(ctx context.Context, request tododto.CreateTodoRequest) (tododto.Todo, error) {
	return s.create.Handle(ctx, request)
}

func (s *todoGatewayShim) ListTodos(ctx context.Context, workspaceID, ownerUserID string, filters tododto.ListFilters) ([]tododto.Todo, error) {
	return s.list.Handle(ctx, workspaceID, ownerUserID, filters)
}

func (s *todoGatewayShim) SearchCandidates(ctx context.Context, workspaceID, ownerUserID, keyword string) ([]tododto.Candidate, error) {
	return s.candidates.Handle(ctx, workspaceID, ownerUserID, keyword)
}

func (s *todoGatewayShim) GetTodo(ctx context.Context, workspaceID, ownerUserID, todoID string) (tododto.Todo, error) {
	todo, err := s.get.Handle(ctx, workspaceID, ownerUserID, todoID)
	if errors.Is(err, tododomain.ErrTodoNotFound) {
		return tododto.Todo{}, conversationdomain.ErrTodoNotFound
	}
	if err != nil {
		return tododto.Todo{}, err
	}
	if todo.Status != string(tododomain.StatusPending) {
		return tododto.Todo{}, conversationdomain.ErrTodoNotPending
	}
	return todo, nil
}

func (s *todoGatewayShim) DeleteTodo(ctx context.Context, request tododto.DeleteTodoRequest) (tododto.Todo, error) {
	return s.del.Handle(ctx, request)
}

// selectModel resolves the configured ModelPort; config.Load already
// validated the choice, so an invalid openai_compatible configuration is a
// composition bug and fails loudly.
func selectModel(cfg config.Config) convports.ModelPort {
	if cfg.ModelAdapter == config.ModelAdapterOpenAICompatible {
		adapter, err := openai.New(openai.Config{
			BaseURL:   cfg.ModelBaseURL,
			APIKey:    cfg.ModelAPIKey,
			ModelName: cfg.ModelName,
			Timeout:   cfg.ModelTimeout,
		})
		if err != nil {
			panic(fmt.Sprintf("wiring: invalid model adapter configuration: %v", err))
		}
		return adapter
	}
	return deterministic.New(time.Now)
}

// registerConversationRoutes exposes the conversation and confirmation
// routes behind the auth middleware.
func registerConversationRoutes(cfg config.Config, pool *pgxpool.Pool, mux *http.ServeMux, auth func(http.Handler) http.Handler, todos todoHandlers) {
	confirmations := convpostgres.NewConfirmationStore(pool)
	messageLog := convpostgres.NewMessageLogStore(pool)
	shim := &todoGatewayShim{
		create:     todos.Create,
		del:        todos.Delete,
		list:       &todoquery.ListTodosHandler{Store: todos.Store, Now: time.Now},
		get:        &todoquery.GetTodoHandler{Store: todos.Store, Now: time.Now},
		candidates: &todoquery.SearchCandidatesHandler{Store: todos.Store},
	}
	process := &convcommand.ProcessMessageHandler{
		Model:             selectModel(cfg),
		Todos:             shim,
		Confirmations:     confirmations,
		Messages:          messageLog,
		UoW:               &joinableUoW{runner: database.NewTxRunner(pool)},
		Router:            convapplication.NewRouter(),
		NewConfirmationID: newID,
		Now:               time.Now,
		ConfirmationTTL:   cfg.ConfirmationTTL,
	}
	createConfirmation := &convcommand.CreateConfirmationHandler{
		Todos:           shim,
		Confirmations:   confirmations,
		NewID:           newID,
		Now:             time.Now,
		ConfirmationTTL: cfg.ConfirmationTTL,
	}
	confirmAction := &convcommand.ConfirmActionHandler{
		Confirmations: confirmations,
		Todos:         shim,
		UoW:           &joinableUoW{runner: database.NewTxRunner(pool)},
		Now:           time.Now,
	}
	conversationhttp.RegisterRoutes(mux, auth, &conversationhttp.Handler{
		ProcessMessage:     process,
		CreateConfirmation: createConfirmation,
		ConfirmAction:      confirmAction,
	})
}

// noopDeliveryStore is the INTERIM DeliveryStore keeping the API compiling
// until Task 7 lands the real postgres implementation and Task 12 rewires it.
// Writes are inert and every read reports "not found"/empty, which preserves
// the ITER-0002 behavior: plans are persisted, no deliveries are tracked yet.
type noopDeliveryStore struct{}

func (noopDeliveryStore) Save(context.Context, reminderdomain.ReminderDelivery) error { return nil }

func (noopDeliveryStore) Update(context.Context, reminderdomain.ReminderDelivery) error { return nil }

func (noopDeliveryStore) ByIdempotencyKey(context.Context, string, string) (reminderdomain.ReminderDelivery, error) {
	return reminderdomain.ReminderDelivery{}, reminderdomain.ErrDeliveryNotFound
}

func (noopDeliveryStore) ByProviderMessageID(context.Context, string) (reminderdomain.ReminderDelivery, error) {
	return reminderdomain.ReminderDelivery{}, reminderdomain.ErrDeliveryNotFound
}

func (noopDeliveryStore) SetProviderJobID(context.Context, string, string, int64) error { return nil }

func (noopDeliveryStore) PlannedJobIDs(context.Context, string, string, int) ([]int64, error) {
	return nil, nil
}

func (noopDeliveryStore) Stats(context.Context, string) (reminderdto.DeliveryCounts, error) {
	return reminderdto.DeliveryCounts{}, nil
}

func (noopDeliveryStore) List(context.Context, string, reminderdto.DeliveryFilter) ([]reminderdomain.ReminderDelivery, error) {
	return nil, nil
}

// reminderPlannerShim adapts Reminder's public application handlers to Todo's
// consumer-owned ReminderPlanner port (D1). It exists only in cmd: neither
// context imports the other.
type reminderPlannerShim struct {
	plan   *remindercommand.PlanReminderHandler
	revoke *remindercommand.RevokePlansHandler
}

func (s *reminderPlannerShim) Plan(ctx context.Context, request todoports.PlanReminderRequest) error {
	return s.plan.Handle(ctx, reminderdto.PlanRequest{
		WorkspaceID:         request.WorkspaceID,
		TodoID:              request.TodoID,
		TodoReminderVersion: request.TodoReminderVersion,
		ScheduledAtUTC:      request.ScheduledAtUTC,
		Channels:            request.Channels,
	})
}

func (s *reminderPlannerShim) Revoke(ctx context.Context, request todoports.RevokeReminderRequest) error {
	return s.revoke.Handle(ctx, reminderdto.RevokeRequest{
		WorkspaceID:         request.WorkspaceID,
		TodoID:              request.TodoID,
		UpToReminderVersion: request.UpToReminderVersion,
	})
}

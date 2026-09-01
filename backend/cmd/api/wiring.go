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
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	riverqueue "github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

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
	identitydto "github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/query"
	identitydomain "github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
	portabilityhttp "github.com/Xin98/artificial-brain/backend/internal/modules/portability/adapters/inbound/http"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/adapters/outbound/archive"
	portabilitypostgres "github.com/Xin98/artificial-brain/backend/internal/modules/portability/adapters/outbound/postgres"
	portabilitycommand "github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/command"
	portabilitydto "github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	portabilityports "github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	portabilityquery "github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/query"
	reminderhttp "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/inbound/http"
	aliyunprovider "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/outbound/aliyun"
	reminderpostgres "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/outbound/postgres"
	riversched "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/outbound/river"
	remindercommand "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/command"
	reminderdto "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	reminderports "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	reminderquery "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/query"
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

// buildHandler composes the API handler: health routes, identity, todo, and
// reminder routes behind the session middleware, and the gated dev inboxes.
// It is the single composition seam so the wiring is exercised by the
// composition integration test.
func buildHandler(cfg config.Config, pool *pgxpool.Pool, ready server.Readiness, checker *systemhealth.Checker) http.Handler {
	mux := http.NewServeMux()
	server.RegisterHealthRoutes(mux, ready, checker)
	auth := newAuthMiddleware(pool)
	registerIdentityRoutes(cfg, pool, mux, auth)
	// Insert-only River client: the API only enqueues reminder jobs and never
	// works them, so the client is never started.
	riverClient, err := riverqueue.NewClient(riverpgxv5.New(pool), &riverqueue.Config{})
	if err != nil {
		panic(fmt.Sprintf("wiring: invalid river client configuration: %v", err))
	}
	todos := buildTodoHandlers(pool, riversched.New(riverClient),
		channelsProvider(identitypostgres.NewChannelStore(pool)), time.Now)
	registerTodoRoutes(mux, auth, todos)
	registerConversationRoutes(cfg, pool, mux, auth, todos)
	registerReminderRoutes(cfg, pool, mux, auth, todos.Deliveries)
	registerPortabilityRoutes(cfg, mux, auth, buildPortabilityHandlers(cfg, pool, time.Now))
	if cfg.DevInboxEnabled && cfg.AppEnv != config.AppEnvProduction {
		mux.Handle("GET /api/v1/dev/sms-inbox", identityhttp.NewDevInboxHandler(identitypostgres.NewOutboxReader(pool)))
	}
	if cfg.ReminderDevOutboxEnabled && cfg.AppEnv != config.AppEnvProduction {
		mux.Handle("GET /api/v1/dev/reminder-outbox",
			reminderhttp.NewDevOutboxHandler(devOutboxStore{reader: reminderpostgres.NewOutboxReader(pool)}))
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
		RequestLoginChallenge: phoneLoginRequester{request: &command.RequestLoginChallengeHandler{
			Challenges:        challenges,
			Outbox:            outbox,
			NewCode:           newSixDigitCode,
			NewID:             newID,
			Now:               time.Now,
			ChallengeTTL:      cfg.LoginChallengeTTL,
			PrivateAdminPhone: cfg.PrivateAdminPhone,
		}},
		VerifyLoginChallenge: phoneLoginVerifier{verify: &command.VerifyLoginChallengeHandler{
			Challenges:        challenges,
			Users:             users,
			Workspaces:        workspaces,
			Sessions:          sessions,
			NewID:             newID,
			NewToken:          newSessionToken,
			Now:               time.Now,
			SessionTTL:        cfg.SessionTTL,
			PrivateAdminPhone: cfg.PrivateAdminPhone,
		}},
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

// phoneLoginRequester adapts the dual-identifier request command to the
// phone-only HTTP seam; the HTTP layer moves to identifiers in a later task.
type phoneLoginRequester struct {
	request *command.RequestLoginChallengeHandler
}

func (a phoneLoginRequester) Handle(ctx context.Context, phone string) error {
	return a.request.Handle(ctx, identitydomain.LoginIdentifier{Phone: phone})
}

// phoneLoginVerifier adapts the dual-identifier verify command to the
// phone-only HTTP seam; the HTTP layer moves to identifiers in a later task.
type phoneLoginVerifier struct {
	verify *command.VerifyLoginChallengeHandler
}

func (a phoneLoginVerifier) Handle(ctx context.Context, phone, code string) (identitydto.VerifyLoginChallengeResult, error) {
	return a.verify.Handle(ctx, identitydomain.LoginIdentifier{Phone: phone}, code)
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
	Store      todoports.TodoStore
	Deliveries reminderports.DeliveryStore
	Create     *todocommand.CreateTodoHandler
	Complete   *todocommand.CompleteTodoHandler
	Delete     *todocommand.DeleteTodoHandler
	Update     *todocommand.UpdateTodoHandler
}

// buildTodoHandlers wires the Todo commands. The scheduler and the channels
// provider are parameters so the composition test can prove atomic rollback
// with a failing fake and drive the channel snapshot deterministically.
func buildTodoHandlers(pool *pgxpool.Pool, scheduler reminderports.JobScheduler, channels todoports.ChannelsProvider, now func() time.Time) todoHandlers {
	uow := &joinableUoW{runner: database.NewTxRunner(pool)}
	todos := todopostgres.NewStore(pool)
	plans := reminderpostgres.NewPlanStore(pool)
	deliveries := reminderpostgres.NewDeliveryStore(pool)
	planner := &reminderPlannerShim{
		plan:   &remindercommand.PlanReminderHandler{Plans: plans, Deliveries: deliveries, Scheduler: scheduler, NewID: newID, Now: now},
		revoke: &remindercommand.RevokePlansHandler{Plans: plans, Deliveries: deliveries, Scheduler: scheduler, Log: slog.Default(), Now: now},
	}
	return todoHandlers{
		Store:      todos,
		Deliveries: deliveries,
		Create:     &todocommand.CreateTodoHandler{Store: todos, UoW: uow, Planner: planner, Channels: channels, NewID: newID, Now: now},
		Complete:   &todocommand.CompleteTodoHandler{Store: todos, UoW: uow, Planner: planner, Now: now},
		Delete:     &todocommand.DeleteTodoHandler{Store: todos, UoW: uow, Planner: planner, Now: now},
		Update:     &todocommand.UpdateTodoHandler{Store: todos, UoW: uow, Planner: planner, Now: now},
	}
}

// channelsProvider snapshots the owner's usable (verified+enabled) contact
// channel kinds, workspace+user scoped and deterministically sorted, so
// reminder plans carry a stable requested-channel snapshot.
func channelsProvider(channels *identitypostgres.ChannelStore) todoports.ChannelsProvider {
	return func(ctx context.Context, workspaceID, ownerUserID string) ([]string, error) {
		rows, err := channels.ListByUser(ctx, workspaceID, ownerUserID)
		if err != nil {
			return nil, err
		}
		usable := make(map[string]bool)
		for _, row := range rows {
			if row.Usable() {
				usable[string(row.Kind)] = true
			}
		}
		snapshot := make([]string, 0, len(usable))
		for kind := range usable {
			snapshot = append(snapshot, kind)
		}
		sort.Strings(snapshot)
		return snapshot, nil
	}
}

// reminderStats adapts the delivery store's workspace counts to Todo's
// ReminderStats seam; the dashboard surfaces the four terminal/retrying
// buckets.
func reminderStats(deliveries reminderports.DeliveryStore) todoports.ReminderStats {
	return func(ctx context.Context, workspaceID string) (todoports.ReminderCounts, error) {
		counts, err := deliveries.Stats(ctx, workspaceID)
		if err != nil {
			return todoports.ReminderCounts{}, err
		}
		return todoports.ReminderCounts{
			Succeeded:  counts.Succeeded,
			Retrying:   counts.Retrying,
			Failed:     counts.Failed,
			Suppressed: counts.Suppressed,
		}, nil
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
		Dashboard: &todoquery.DashboardSummaryHandler{Store: handlers.Store, Now: time.Now, ReminderStats: reminderStats(handlers.Deliveries)},
	})
}

// registerReminderRoutes exposes the reminder delivery list and the ops
// snapshot behind the auth middleware; the receipt webhook authenticates by
// HMAC signature instead of a session. The receipt parser follows the
// configured SMS provider.
func registerReminderRoutes(cfg config.Config, pool *pgxpool.Pool, mux *http.ServeMux, auth func(http.Handler) http.Handler, deliveries reminderports.DeliveryStore) {
	parse := reminderhttp.ParseGenericReceipt
	if cfg.ReminderSmsAdapter == config.ReminderSmsAdapterAliyun {
		parse = aliyunprovider.ParseSmsReport
	}
	reminderhttp.RegisterRoutes(mux, auth, &reminderhttp.Handler{
		List:    &reminderquery.ListDeliveriesHandler{Deliveries: deliveries},
		Ops:     &reminderquery.ReminderOpsHandler{Ops: reminderpostgres.NewOpsStore(pool), Now: time.Now},
		Receipt: &remindercommand.RecordReceiptHandler{Deliveries: deliveries, Log: slog.Default(), Now: time.Now},
		Parse:   parse,
		Secret:  cfg.ReminderReceiptSecret,
	})
}

// devOutboxStore adapts the postgres outbox reader's rows to the reminder
// dev outbox's message shape.
type devOutboxStore struct {
	reader *reminderpostgres.OutboxReader
}

func (s devOutboxStore) LatestByAddress(ctx context.Context, address string, limit int) ([]reminderhttp.DevOutboxMessage, error) {
	rows, err := s.reader.LatestByAddress(ctx, address, limit)
	if err != nil {
		return nil, err
	}
	messages := make([]reminderhttp.DevOutboxMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, reminderhttp.DevOutboxMessage{
			Address:   row.Address,
			Channel:   row.Channel,
			TodoID:    row.TodoID,
			Body:      row.Body,
			CreatedAt: row.CreatedAt,
		})
	}
	return messages, nil
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
	// Pending and completed todos are both deletable through the
	// confirmation-gated flow; an already soft-deleted todo is
	// indistinguishable from a missing one at this seam.
	if todo.Status == string(tododomain.StatusDeleted) {
		return tododto.Todo{}, conversationdomain.ErrTodoNotFound
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
		Title:               request.Title,
		OwnerUserID:         request.OwnerUserID,
	})
}

func (s *reminderPlannerShim) Revoke(ctx context.Context, request todoports.RevokeReminderRequest) error {
	return s.revoke.Handle(ctx, reminderdto.RevokeRequest{
		WorkspaceID:         request.WorkspaceID,
		TodoID:              request.TodoID,
		UpToReminderVersion: request.UpToReminderVersion,
		Reason:              request.Reason,
	})
}

// portabilityHandlers groups the four portability HTTP handlers: the export
// bundle download and the two-phase import — upload, get, confirm.
type portabilityHandlers struct {
	Export  *portabilitycommand.ExportBundleHandler
	Upload  *portabilitycommand.UploadImportHandler
	Confirm *portabilitycommand.ConfirmImportHandler
	Get     *portabilityquery.GetImportQuery
}

// buildPortabilityHandlers wires the portability application against the
// instance identity seam, the archive adapter, and the other modules' public
// handlers (adapted by the cmd-local shims below). The confirm handler joins
// the joinable UoW so a whole import commits as exactly one transaction.
func buildPortabilityHandlers(cfg config.Config, pool *pgxpool.Pool, now func() time.Time) portabilityHandlers {
	todos := todopostgres.NewStore(pool)
	channels := identitypostgres.NewChannelStore(pool)
	deliveries := reminderpostgres.NewDeliveryStore(pool)
	imports := portabilitypostgres.NewImportStore(pool)
	sources := portabilitypostgres.NewSourceRecordStore(pool)
	parser := archive.NewParser()

	return portabilityHandlers{
		Export: &portabilitycommand.ExportBundleHandler{
			Instance:   portabilitypostgres.NewMetaStore(pool),
			Todos:      &todoExportShim{export: &todoquery.ExportTodosHandler{Store: todos, Now: now}},
			Channels:   &channelExportShim{q: &query.ChannelsExportQuery{Channels: channels}},
			Deliveries: &deliveryExportShim{q: &reminderquery.ExportDeliveriesHandler{Deliveries: deliveries}},
			Archive:    archive.Factory(),
			PageSize:   200,
			Now:        now,
		},
		Upload: &portabilitycommand.UploadImportHandler{
			Imports:   imports,
			Sources:   sources,
			Parser:    parser,
			NewID:     newID,
			Now:       now,
			ImportTTL: 24 * time.Hour,
		},
		Confirm: &portabilitycommand.ConfirmImportHandler{
			Imports:    imports,
			Sources:    sources,
			Parser:     parser,
			Todos:      &todoImportShim{imp: &todocommand.ImportTodoHandler{Store: todos, NewID: newID, Now: now}},
			Channels:   &channelImportShim{imp: &command.ImportChannelHandler{Channels: channels, NewID: newID, Now: now}, list: &query.ChannelsQuery{Channels: channels}},
			Deliveries: &deliveryImportShim{imp: &remindercommand.ImportDeliveriesHandler{Deliveries: deliveries, NewID: newID, Now: now}},
			UoW:        &joinableUoW{runner: database.NewTxRunner(pool)},
			Log:        slog.Default(),
			NewID:      newID,
			Now:        now,
			ImportTTL:  24 * time.Hour,
		},
		Get: &portabilityquery.GetImportQuery{
			Imports:   imports,
			Now:       now,
			ImportTTL: 24 * time.Hour,
		},
	}
}

// registerPortabilityRoutes exposes the export and two-phase import routes
// behind the auth middleware; the configured bundle size cap guards uploads.
func registerPortabilityRoutes(cfg config.Config, mux *http.ServeMux, auth func(http.Handler) http.Handler, handlers portabilityHandlers) {
	portabilityhttp.RegisterRoutes(mux, auth, &portabilityhttp.Handler{
		Export:         handlers.Export,
		Upload:         handlers.Upload,
		Confirm:        handlers.Confirm,
		Get:            handlers.Get,
		MaxBundleBytes: int64(cfg.PortabilityMaxBundleBytes),
	})
}

// todoExportShim adapts Todo's public export handler to Portability's
// consumer-owned TodoExporter port. It exists only in cmd: neither context
// imports the other.
type todoExportShim struct {
	export *todoquery.ExportTodosHandler
}

func (s *todoExportShim) ExportTodos(ctx context.Context, workspaceID, userID string, offset, limit int) ([]portabilitydto.TodoExportRecord, error) {
	records, err := s.export.Handle(ctx, workspaceID, userID, offset, limit)
	if err != nil {
		return nil, err
	}
	mapped := make([]portabilitydto.TodoExportRecord, 0, len(records))
	for _, record := range records {
		mapped = append(mapped, portabilitydto.TodoExportRecord{
			ID:              record.ID,
			Title:           record.Title,
			Description:     record.Description,
			DueAtUTC:        record.DueAtUTC,
			TimezoneAtInput: record.TimezoneAtInput,
			Status:          record.Status,
			ReminderVersion: record.ReminderVersion,
			CreatedAt:       record.CreatedAt,
			UpdatedAt:       record.UpdatedAt,
			CompletedAt:     record.CompletedAt,
			DeletedAt:       record.DeletedAt,
		})
	}
	return mapped, nil
}

// channelExportShim adapts Identity's channel preferences query to
// Portability's ChannelExporter port, mapping the principal across.
type channelExportShim struct {
	q *query.ChannelsExportQuery
}

func (s *channelExportShim) ExportChannels(ctx context.Context, principal portabilityports.Principal) ([]portabilitydto.ChannelExportRecord, error) {
	prefs, err := s.q.GetChannelPreferences(ctx, identitydto.Principal{
		UserID:      principal.UserID,
		WorkspaceID: principal.WorkspaceID,
	})
	if err != nil {
		return nil, err
	}
	mapped := make([]portabilitydto.ChannelExportRecord, 0, len(prefs))
	for _, pref := range prefs {
		mapped = append(mapped, portabilitydto.ChannelExportRecord{
			ID:      pref.ID,
			Kind:    pref.Kind,
			Address: pref.Address,
			Enabled: pref.Enabled,
		})
	}
	return mapped, nil
}

// deliveryExportShim adapts Reminder's public export handler to
// Portability's DeliveryExporter port; the stored todo id becomes the
// bundle's source todo record id.
type deliveryExportShim struct {
	q *reminderquery.ExportDeliveriesHandler
}

func (s *deliveryExportShim) ExportDeliveries(ctx context.Context, workspaceID string, offset, limit int) ([]portabilitydto.DeliveryExportRecord, error) {
	records, err := s.q.Handle(ctx, workspaceID, offset, limit)
	if err != nil {
		return nil, err
	}
	mapped := make([]portabilitydto.DeliveryExportRecord, 0, len(records))
	for _, record := range records {
		mapped = append(mapped, portabilitydto.DeliveryExportRecord{
			ID:                 record.ID,
			SourceTodoRecordID: record.TodoID,
			Channel:            record.Channel,
			State:              record.State,
			SuppressionReason:  record.SuppressionReason,
			AttemptCount:       record.AttemptCount,
			ProviderMessageID:  record.ProviderMessageID,
			LastErrorCode:      record.LastErrorCode,
			TodoTitleSnapshot:  record.TodoTitleSnapshot,
			ScheduledAt:        record.ScheduledAt,
			CreatedAt:          record.CreatedAt,
			SubmittedAt:        record.SubmittedAt,
			FinalizedAt:        record.FinalizedAt,
			ReceiptState:       record.ReceiptState,
			ReceiptErrorCode:   record.ReceiptErrorCode,
			ReceiptAt:          record.ReceiptAt,
			Origin:             record.Origin,
		})
	}
	return mapped, nil
}

// todoImportShim adapts Todo's public import handler to Portability's
// TodoImporter port, adding the principal's identity and returning the
// created todo's id.
type todoImportShim struct {
	imp *todocommand.ImportTodoHandler
}

func (s *todoImportShim) ImportTodo(ctx context.Context, principal portabilityports.Principal, record portabilitydto.TodoImportRequest) (string, error) {
	created, err := s.imp.Handle(ctx, tododto.ImportTodoRequest{
		WorkspaceID:     principal.WorkspaceID,
		UserID:          principal.UserID,
		Title:           record.Title,
		Description:     record.Description,
		DueAtUTC:        record.DueAtUTC,
		TimezoneAtInput: record.TimezoneAtInput,
		Status:          record.Status,
		ReminderVersion: record.ReminderVersion,
		Version:         record.Version,
		CreatedAt:       record.CreatedAt,
		UpdatedAt:       record.UpdatedAt,
		CompletedAt:     record.CompletedAt,
		DeletedAt:       record.DeletedAt,
	})
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

// channelImportShim adapts Identity's public import handler to Portability's
// ChannelImporter port. A duplicate (user, kind, address) reports the
// portability-local ErrChannelExists sentinel WITH the existing channel's id
// resolved through the channels query, so confirm downgrades the record to
// skipped and registers it against the existing target (T9).
type channelImportShim struct {
	imp  *command.ImportChannelHandler
	list *query.ChannelsQuery
}

func (s *channelImportShim) ImportChannel(ctx context.Context, principal portabilityports.Principal, record portabilitydto.ChannelImportRequest) (string, error) {
	identityPrincipal := identitydto.Principal{UserID: principal.UserID, WorkspaceID: principal.WorkspaceID}
	// Detect duplicates BEFORE the insert: confirm runs inside one unit-of-
	// work transaction, and a failed insert aborts it ("current transaction
	// is aborted"), rolling back the whole import. Reading the existing
	// channels joins the same transaction and keeps the downgrade path
	// transaction-clean — a self-import skips duplicate channels instead of
	// failing.
	if existingID, found := s.findExisting(ctx, identityPrincipal, record.Kind, record.Address); found {
		return existingID, portabilityports.ErrChannelExists
	}
	view, err := s.imp.Handle(ctx, identityPrincipal, record.Kind, record.Address, record.Enabled)
	if err == nil {
		return view.ID, nil
	}
	if !errors.Is(err, identitydomain.ErrChannelExists) {
		return "", err
	}
	// Concurrent duplicate between the check and the insert. The ambient
	// transaction is already aborted here, so resolving the existing id is
	// best-effort: report the sentinel with whatever id the listing still
	// yields, and let confirm surface the raw error otherwise.
	existing, listErr := s.list.GetContactChannels(ctx, identityPrincipal)
	if listErr != nil {
		return "", err
	}
	for _, channel := range existing {
		if channel.Kind == record.Kind && channel.Address == record.Address {
			return channel.ID, portabilityports.ErrChannelExists
		}
	}
	return "", err
}

// findExisting resolves an existing (user, kind, address) channel id inside
// the ambient transaction; found is false when the listing fails or carries
// no match (the caller then attempts the insert and maps the outcome).
func (s *channelImportShim) findExisting(ctx context.Context, principal identitydto.Principal, kind, address string) (string, bool) {
	existing, err := s.list.GetContactChannels(ctx, principal)
	if err != nil {
		return "", false
	}
	for _, channel := range existing {
		if channel.Kind == kind && channel.Address == address {
			return channel.ID, true
		}
	}
	return "", false
}

// deliveryImportShim adapts Reminder's public import handler to
// Portability's DeliveryImporter port, adding the principal's identity.
type deliveryImportShim struct {
	imp *remindercommand.ImportDeliveriesHandler
}

func (s *deliveryImportShim) ImportDelivery(ctx context.Context, principal portabilityports.Principal, record portabilitydto.DeliveryImportRequest) error {
	return s.imp.Handle(ctx, reminderdto.ImportDeliveryRequest{
		WorkspaceID:         principal.WorkspaceID,
		OwnerUserID:         principal.UserID,
		TodoID:              record.TodoID,
		TodoReminderVersion: record.TodoReminderVersion,
		Channel:             record.Channel,
		State:               record.State,
		SuppressionReason:   record.SuppressionReason,
		ProviderMessageID:   record.ProviderMessageID,
		LastErrorCode:       record.LastErrorCode,
		AttemptCount:        record.AttemptCount,
		TodoTitleSnapshot:   record.TodoTitleSnapshot,
		ScheduledAt:         record.ScheduledAt,
		CreatedAt:           record.CreatedAt,
		SubmittedAt:         record.SubmittedAt,
		FinalizedAt:         record.FinalizedAt,
		ReceiptState:        record.ReceiptState,
		ReceiptErrorCode:    record.ReceiptErrorCode,
		ReceiptAt:           record.ReceiptAt,
		SourceInstanceID:    record.SourceInstanceID,
		SourceRecordID:      record.SourceRecordID,
	})
}

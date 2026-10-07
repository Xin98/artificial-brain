package http

import (
	"context"
	identity "github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/observability"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

type accountCreator interface {
	Handle(context.Context, dto.CreateAccountRequest) (dto.AccountView, error)
}
type accountGetter interface {
	Handle(context.Context, domain.Scope, string) (dto.AccountView, error)
}
type lister interface {
	Handle(context.Context, dto.ListRequest) (dto.Page, error)
}
type statusGetter interface {
	Handle(context.Context, domain.Scope, string) (dto.DataStatus, error)
}
type analyzer interface {
	Handle(context.Context, dto.AnalysisRequest) (dto.AnalysisView, error)
}
type universeCreator interface {
	Handle(context.Context, dto.CreateUniverseVersionRequest) (dto.VersionView, error)
}
type strategyCreator interface {
	Handle(context.Context, dto.CreateStrategyVersionRequest) (dto.VersionView, error)
}
type orderPlacer interface {
	Handle(context.Context, dto.PlaceOrderRequest) (dto.OrderView, error)
}
type orderCanceller interface {
	Handle(context.Context, dto.CancelOrderRequest) (dto.OrderView, error)
}
type automationWriter interface {
	Handle(context.Context, dto.ConfigureAutomationRequest) (dto.AccountView, error)
}
type syncStarter interface {
	Handle(context.Context, command.StartSyncRequest) (dto.RunView, error)
}
type syncGetter interface {
	Handle(context.Context, domain.Scope, string) (dto.RunView, error)
}
type evaluator interface {
	Start(context.Context, dto.EvaluateRequest) (dto.RunView, error)
}
type evaluationGetter interface {
	Handle(context.Context, domain.Scope, string, string) (dto.EvaluationReadView, error)
}
type backtestCreator interface {
	Handle(context.Context, dto.CreateBacktestRequest) (dto.RunView, error)
}
type backtestGetter interface {
	Handle(context.Context, domain.Scope, string) (dto.BacktestView, error)
}
type performanceGetter interface {
	Handle(context.Context, domain.Scope, string) (dto.PerformanceView, error)
}
type Handler struct {
	Mode           string
	CreateAccount  accountCreator
	Account        accountGetter
	List           lister
	Instruments    lister
	DataStatus     statusGetter
	Analysis       analyzer
	CreateUniverse universeCreator
	CreateStrategy strategyCreator
	PlaceOrder     orderPlacer
	CancelOrder    orderCanceller
	Automation     automationWriter
	StartSync      syncStarter
	Sync           syncGetter
	Evaluate       evaluator
	Evaluation     evaluationGetter
	CreateBacktest backtestCreator
	Backtest       backtestGetter
	Performance    performanceGetter
}

func RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler, h *Handler) {
	routes := map[string]http.HandlerFunc{
		"GET /api/v1/investment/data-status":                            h.status,
		"POST /api/v1/investment/data-sync":                             h.startSync,
		"GET /api/v1/investment/data-sync/{id}":                         h.syncRun,
		"GET /api/v1/investment/instruments":                            h.instruments,
		"GET /api/v1/investment/instruments/{id}/analysis":              h.analysis,
		"POST /api/v1/investment/universes":                             h.createUniverse,
		"POST /api/v1/investment/universes/{id}/versions":               h.createUniverse,
		"POST /api/v1/investment/strategies/{id}/versions":              h.createStrategy,
		"POST /api/v1/investment/accounts":                              h.createAccount,
		"GET /api/v1/investment/accounts/{id}":                          h.account,
		"GET /api/v1/investment/accounts/{id}/performance":              h.performance,
		"POST /api/v1/investment/accounts/{id}/orders":                  h.placeOrder,
		"POST /api/v1/investment/accounts/{id}/orders/{orderId}/cancel": h.cancelOrder,
		"PUT /api/v1/investment/accounts/{id}/automation":               h.automation,
		"POST /api/v1/investment/accounts/{id}/evaluations":             h.evaluate,
		"GET /api/v1/investment/accounts/{id}/evaluations/{runId}":      h.evaluation,
		"POST /api/v1/investment/backtests":                             h.createBacktest,
		"GET /api/v1/investment/backtests/{id}":                         h.backtest,
	}
	for _, resource := range []string{"universes", "strategies", "accounts", "backtests", "data-syncs"} {
		resource := resource
		routes["GET /api/v1/investment/"+resource] = func(w http.ResponseWriter, r *http.Request) { h.list(w, r, resource, "") }
	}
	for _, resource := range []string{"positions", "ledger", "orders", "evaluations", "automation-events"} {
		resource := resource
		routes["GET /api/v1/investment/accounts/{id}/"+resource] = func(w http.ResponseWriter, r *http.Request) { h.list(w, r, resource, r.PathValue("id")) }
	}
	for pattern, handler := range routes {
		mux.Handle(pattern, auth(handler))
	}
}
func scopeFrom(w http.ResponseWriter, r *http.Request) (domain.Scope, bool) {
	p, ok := identity.PrincipalFromContext(r.Context())
	if !ok {
		writeJSON(w, 401, errorResponse{Code: "unauthenticated", Message: "authentication required", CorrelationID: observability.CorrelationID(r.Context())})
		return domain.Scope{}, false
	}
	return domain.Scope{WorkspaceID: p.WorkspaceID, OwnerUserID: p.UserID}, true
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validID(w http.ResponseWriter, r *http.Request, id string) bool {
	if !uuidPattern.MatchString(id) {
		writeError(w, r, domain.ErrInvalidInput)
		return false
	}
	return true
}
func mutation(w http.ResponseWriter, r *http.Request, scope domain.Scope, m *dto.Mutation) bool {
	key := r.Header.Get("Idempotency-Key")
	if len(key) == 0 || len(key) > 200 {
		writeError(w, r, domain.ErrInvalidInput)
		return false
	}
	m.Scope = scope
	m.Route = r.Method + " " + r.URL.Path
	m.Key = key
	return true
}
func queryAllowed(w http.ResponseWriter, r *http.Request, names ...string) bool {
	allowed := map[string]bool{}
	for _, n := range names {
		allowed[n] = true
	}
	for name, values := range r.URL.Query() {
		if !allowed[name] || len(values) != 1 {
			writeError(w, r, domain.ErrInvalidInput)
			return false
		}
	}
	return true
}
func listRequest(w http.ResponseWriter, r *http.Request, scope domain.Scope, resource, parent string) (dto.ListRequest, bool) {
	q := dto.ListRequest{Scope: scope, Resource: resource, ParentID: parent, Cursor: r.URL.Query().Get("cursor")}
	raw := r.URL.Query().Get("limit")
	if raw != "" {
		v, e := strconv.Atoi(raw)
		if e != nil || v < 1 || v > 100 {
			writeError(w, r, domain.ErrInvalidInput)
			return q, false
		}
		q.Limit = v
	}
	return q, true
}
func reply[T any](w http.ResponseWriter, r *http.Request, status int, value T, e error) {
	if e != nil {
		writeError(w, r, e)
		return
	}
	writeJSON(w, status, value)
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request, resource, parent string) {
	scope, ok := scopeFrom(w, r)
	if !ok || !queryAllowed(w, r, "cursor", "limit") {
		return
	}
	if parent != "" && !validID(w, r, parent) {
		return
	}
	request, ok := listRequest(w, r, scope, resource, parent)
	if !ok {
		return
	}
	value, e := h.List.Handle(r.Context(), request)
	reply(w, r, 200, value, e)
}
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok || !queryAllowed(w, r) {
		return
	}
	v, e := h.DataStatus.Handle(r.Context(), scope, h.Mode)
	reply(w, r, 200, v, e)
}
func (h *Handler) account(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok || !validID(w, r, r.PathValue("id")) || !queryAllowed(w, r) {
		return
	}
	v, e := h.Account.Handle(r.Context(), scope, r.PathValue("id"))
	reply(w, r, 200, v, e)
}
func (h *Handler) performance(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok || !validID(w, r, r.PathValue("id")) || !queryAllowed(w, r) {
		return
	}
	v, e := h.Performance.Handle(r.Context(), scope, r.PathValue("id"))
	reply(w, r, 200, v, e)
}
func (h *Handler) backtest(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok || !validID(w, r, r.PathValue("id")) || !queryAllowed(w, r) {
		return
	}
	v, e := h.Backtest.Handle(r.Context(), scope, r.PathValue("id"))
	reply(w, r, 200, v, e)
}
func (h *Handler) syncRun(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok || !validID(w, r, r.PathValue("id")) || !queryAllowed(w, r) {
		return
	}
	v, e := h.Sync.Handle(r.Context(), scope, r.PathValue("id"))
	reply(w, r, 200, v, e)
}
func (h *Handler) evaluation(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok || !validID(w, r, r.PathValue("id")) || !validID(w, r, r.PathValue("runId")) || !queryAllowed(w, r) {
		return
	}
	v, e := h.Evaluation.Handle(r.Context(), scope, r.PathValue("id"), r.PathValue("runId"))
	reply(w, r, 200, v, e)
}
func (h *Handler) instruments(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok || !queryAllowed(w, r, "cursor", "limit", "search", "risk", "potential", "accountId") {
		return
	}
	account := r.URL.Query().Get("accountId")
	if account != "" && !validID(w, r, account) {
		return
	}
	request, ok := listRequest(w, r, scope, "instruments", account)
	if !ok {
		return
	}
	request.Search = r.URL.Query().Get("search")
	request.Risk = r.URL.Query().Get("risk")
	request.Potential = r.URL.Query().Get("potential")
	v, e := h.Instruments.Handle(r.Context(), request)
	reply(w, r, 200, v, e)
}
func (h *Handler) analysis(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok || !queryAllowed(w, r, "accountId", "asOf") {
		return
	}
	account := r.URL.Query().Get("accountId")
	if account != "" && !validID(w, r, account) {
		return
	}
	request := dto.AnalysisRequest{Scope: scope, InstrumentID: r.PathValue("id"), AccountID: account}
	if len(request.InstrumentID) > 200 || request.InstrumentID == "" {
		writeError(w, r, domain.ErrInvalidInput)
		return
	}
	if raw := r.URL.Query().Get("asOf"); raw != "" {
		v, e := time.Parse(time.RFC3339, raw)
		if e != nil {
			writeError(w, r, domain.ErrInvalidInput)
			return
		}
		request.AsOf = v
	}
	v, e := h.Analysis.Handle(r.Context(), request)
	reply(w, r, 200, v, e)
}

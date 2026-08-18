package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identitydto "github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	tododto "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

var testPrincipal = identitydto.Principal{UserID: "user-1", WorkspaceID: "ws-1", SessionID: "session-1"}

func allowAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(identitydto.WithPrincipal(r.Context(), testPrincipal)))
	})
}

func denyAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "unauthenticated"})
	})
}

type fakeCreator struct {
	request *tododto.CreateTodoRequest
	result  tododto.Todo
	err     error
}

func (f *fakeCreator) Handle(_ context.Context, request tododto.CreateTodoRequest) (tododto.Todo, error) {
	f.request = &request
	return f.result, f.err
}

type fakeCompleter struct {
	request *tododto.CompleteTodoRequest
	result  tododto.Todo
	err     error
}

func (f *fakeCompleter) Handle(_ context.Context, request tododto.CompleteTodoRequest) (tododto.Todo, error) {
	f.request = &request
	return f.result, f.err
}

type fakeUpdater struct {
	request *tododto.UpdateTodoRequest
	result  tododto.Todo
	err     error
}

func (f *fakeUpdater) Handle(_ context.Context, request tododto.UpdateTodoRequest) (tododto.Todo, error) {
	f.request = &request
	return f.result, f.err
}

type fakeLister struct {
	workspaceID string
	ownerUserID string
	filters     tododto.ListFilters
	result      []tododto.Todo
	err         error
}

func (f *fakeLister) Handle(_ context.Context, workspaceID, ownerUserID string, filters tododto.ListFilters) ([]tododto.Todo, error) {
	f.workspaceID, f.ownerUserID, f.filters = workspaceID, ownerUserID, filters
	return f.result, f.err
}

type fakeGetter struct {
	workspaceID string
	ownerUserID string
	todoID      string
	result      tododto.Todo
	err         error
}

func (f *fakeGetter) Handle(_ context.Context, workspaceID, ownerUserID, todoID string) (tododto.Todo, error) {
	f.workspaceID, f.ownerUserID, f.todoID = workspaceID, ownerUserID, todoID
	return f.result, f.err
}

type fakeDashboard struct {
	workspaceID string
	ownerUserID string
	timezone    string
	result      tododto.DashboardSummary
	err         error
}

func (f *fakeDashboard) Handle(_ context.Context, workspaceID, ownerUserID, timezone string) (tododto.DashboardSummary, error) {
	f.workspaceID, f.ownerUserID, f.timezone = workspaceID, ownerUserID, timezone
	return f.result, f.err
}

func newTestHandler(creator *fakeCreator, completer *fakeCompleter, updater *fakeUpdater, lister *fakeLister, getter *fakeGetter, dashboard *fakeDashboard) *Handler {
	return &Handler{
		Create:    creator,
		Complete:  completer,
		Update:    updater,
		List:      lister,
		Get:       getter,
		Dashboard: dashboard,
	}
}

func serve(t *testing.T, h *Handler, auth func(http.Handler) http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	RegisterRoutes(mux, auth, h)
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	return recorder
}

func decodeEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode body error = %v, body = %s", err, recorder.Body.String())
	}
	return envelope
}

func TestRegisterRoutesRequiresAuthentication(t *testing.T) {
	handler := newTestHandler(&fakeCreator{}, &fakeCompleter{}, &fakeUpdater{}, &fakeLister{}, &fakeGetter{}, &fakeDashboard{})
	routes := []struct {
		method string
		target string
		body   string
	}{
		{http.MethodGet, "/api/v1/todos", ""},
		{http.MethodPost, "/api/v1/todos", `{"title":"x"}`},
		{http.MethodGet, "/api/v1/todos/todo-1", ""},
		{http.MethodPatch, "/api/v1/todos/todo-1", `{"version":1}`},
		{http.MethodPost, "/api/v1/todos/todo-1/complete", `{"version":1}`},
		{http.MethodGet, "/api/v1/dashboard/summary?timezone=UTC", ""},
	}
	for _, route := range routes {
		recorder := serve(t, handler, denyAuth, route.method, route.target, route.body)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401", route.method, route.target, recorder.Code)
		}
		if envelope := decodeEnvelope(t, recorder); envelope["code"] != "unauthenticated" {
			t.Fatalf("%s %s envelope = %#v, want unauthenticated", route.method, route.target, envelope)
		}
	}
}

func TestCreateTodoReturns201WithCamelCaseBody(t *testing.T) {
	creator := &fakeCreator{result: tododto.Todo{
		ID: "todo-1", Title: "提交周报", Status: "pending", Version: 1, ReminderVersion: 1,
		CreatedAt: time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC),
	}}
	handler := newTestHandler(creator, &fakeCompleter{}, &fakeUpdater{}, &fakeLister{}, &fakeGetter{}, &fakeDashboard{})

	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/todos", `{"title":"提交周报"}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201, body = %s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if body["id"] != "todo-1" || body["title"] != "提交周报" || body["status"] != "pending" {
		t.Fatalf("body = %#v", body)
	}
	if _, ok := body["createdAt"]; !ok {
		t.Fatalf("body missing createdAt: %#v", body)
	}
	if creator.request == nil {
		t.Fatal("creator not called")
	}
	if creator.request.WorkspaceID != "ws-1" || creator.request.UserID != "user-1" || creator.request.Title != "提交周报" {
		t.Fatalf("creator request = %#v", creator.request)
	}
	if creator.request.DueAtUTC != nil || creator.request.Description != nil {
		t.Fatalf("creator optional fields = %#v, want nil", creator.request)
	}
}

func TestCreateTodoParsesOptionalFields(t *testing.T) {
	creator := &fakeCreator{}
	handler := newTestHandler(creator, &fakeCompleter{}, &fakeUpdater{}, &fakeLister{}, &fakeGetter{}, &fakeDashboard{})

	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/todos",
		`{"title":"提交周报","description":"周五前","dueAtUtc":"2026-08-19T15:00:00Z","timezoneAtInput":"Asia/Shanghai"}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201, body = %s", recorder.Code, recorder.Body.String())
	}
	request := creator.request
	if request == nil || request.Description == nil || *request.Description != "周五前" {
		t.Fatalf("description = %#v", request)
	}
	wantDue := time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC)
	if request.DueAtUTC == nil || !request.DueAtUTC.Equal(wantDue) {
		t.Fatalf("due = %v, want %v", request.DueAtUTC, wantDue)
	}
	if request.TimezoneAtInput == nil || *request.TimezoneAtInput != "Asia/Shanghai" {
		t.Fatalf("timezoneAtInput = %#v", request.TimezoneAtInput)
	}
}

func TestCreateTodoRejectsInvalidTitleAndMalformedDue(t *testing.T) {
	creator := &fakeCreator{err: domain.ErrInvalidTitle}
	handler := newTestHandler(creator, &fakeCompleter{}, &fakeUpdater{}, &fakeLister{}, &fakeGetter{}, &fakeDashboard{})

	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/todos", `{"title":""}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty title status = %d, want 422", recorder.Code)
	}
	if envelope := decodeEnvelope(t, recorder); envelope["code"] != "validation_error" {
		t.Fatalf("envelope = %#v, want validation_error", envelope)
	}

	malformed := &fakeCreator{}
	handler = newTestHandler(malformed, &fakeCompleter{}, &fakeUpdater{}, &fakeLister{}, &fakeGetter{}, &fakeDashboard{})
	recorder = serve(t, handler, allowAuth, http.MethodPost, "/api/v1/todos", `{"title":"x","dueAtUtc":"not-a-time"}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("malformed due status = %d, want 422", recorder.Code)
	}
	if malformed.request != nil {
		t.Fatal("creator called despite malformed due")
	}

	unknown := &fakeCreator{}
	handler = newTestHandler(unknown, &fakeCompleter{}, &fakeUpdater{}, &fakeLister{}, &fakeGetter{}, &fakeDashboard{})
	recorder = serve(t, handler, allowAuth, http.MethodPost, "/api/v1/todos", `{"title":"x","bogus":1}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown field status = %d, want 422", recorder.Code)
	}
}

func TestCompleteTodoReturns200AndMapsConflict(t *testing.T) {
	completer := &fakeCompleter{result: tododto.Todo{ID: "todo-1", Status: "completed", Version: 2}}
	handler := newTestHandler(&fakeCreator{}, completer, &fakeUpdater{}, &fakeLister{}, &fakeGetter{}, &fakeDashboard{})

	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/todos/todo-1/complete", `{"version":1}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("complete status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	if completer.request == nil || completer.request.TodoID != "todo-1" || completer.request.Version != 1 ||
		completer.request.WorkspaceID != "ws-1" || completer.request.UserID != "user-1" {
		t.Fatalf("completer request = %#v", completer.request)
	}

	conflicted := &fakeCompleter{err: domain.ErrConflict}
	handler = newTestHandler(&fakeCreator{}, conflicted, &fakeUpdater{}, &fakeLister{}, &fakeGetter{}, &fakeDashboard{})
	recorder = serve(t, handler, allowAuth, http.MethodPost, "/api/v1/todos/todo-1/complete", `{"version":99}`)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, want 409", recorder.Code)
	}
	if envelope := decodeEnvelope(t, recorder); envelope["code"] != "conflict" {
		t.Fatalf("envelope = %#v, want conflict", envelope)
	}

	missing := &fakeCompleter{}
	handler = newTestHandler(&fakeCreator{}, missing, &fakeUpdater{}, &fakeLister{}, &fakeGetter{}, &fakeDashboard{})
	recorder = serve(t, handler, allowAuth, http.MethodPost, "/api/v1/todos/todo-1/complete", `{}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing version status = %d, want 422", recorder.Code)
	}
	if missing.request != nil {
		t.Fatal("completer called despite missing version")
	}
}

func TestGetTodoMapsNotFoundTo404Envelope(t *testing.T) {
	getter := &fakeGetter{err: domain.ErrTodoNotFound}
	handler := newTestHandler(&fakeCreator{}, &fakeCompleter{}, &fakeUpdater{}, &fakeLister{}, getter, &fakeDashboard{})

	recorder := serve(t, handler, allowAuth, http.MethodGet, "/api/v1/todos/missing", "")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	envelope := decodeEnvelope(t, recorder)
	if envelope["code"] != "not_found" {
		t.Fatalf("envelope = %#v, want not_found", envelope)
	}
	if _, ok := envelope["correlationId"]; !ok {
		t.Fatalf("envelope missing correlationId: %#v", envelope)
	}
	if getter.todoID != "missing" || getter.workspaceID != "ws-1" {
		t.Fatalf("getter args = %#v", getter)
	}
}

func TestUpdateTodoParsesDueChangedSemanticsAndConflict(t *testing.T) {
	updater := &fakeUpdater{result: tododto.Todo{ID: "todo-1", Status: "pending", Version: 2}}
	handler := newTestHandler(&fakeCreator{}, &fakeCompleter{}, updater, &fakeLister{}, &fakeGetter{}, &fakeDashboard{})

	recorder := serve(t, handler, allowAuth, http.MethodPatch, "/api/v1/todos/todo-1", `{"version":1}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	if updater.request == nil || updater.request.DueChanged || updater.request.Version != 1 {
		t.Fatalf("update request = %#v, want DueChanged=false", updater.request)
	}

	recorder = serve(t, handler, allowAuth, http.MethodPatch, "/api/v1/todos/todo-1", `{"version":1,"dueAtUtc":null}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("clear-due status = %d, want 200", recorder.Code)
	}
	if !updater.request.DueChanged || updater.request.DueAtUTC != nil {
		t.Fatalf("clear-due request = %#v, want DueChanged=true nil due", updater.request)
	}

	recorder = serve(t, handler, allowAuth, http.MethodPatch, "/api/v1/todos/todo-1", `{"version":1,"dueAtUtc":"2026-08-20T09:00:00Z"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reschedule status = %d, want 200", recorder.Code)
	}
	wantDue := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	if !updater.request.DueChanged || updater.request.DueAtUTC == nil || !updater.request.DueAtUTC.Equal(wantDue) {
		t.Fatalf("reschedule request = %#v", updater.request)
	}

	conflicted := &fakeUpdater{err: domain.ErrConflict}
	handler = newTestHandler(&fakeCreator{}, &fakeCompleter{}, conflicted, &fakeLister{}, &fakeGetter{}, &fakeDashboard{})
	recorder = serve(t, handler, allowAuth, http.MethodPatch, "/api/v1/todos/todo-1", `{"version":99}`)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, want 409", recorder.Code)
	}

	malformed := &fakeUpdater{}
	handler = newTestHandler(&fakeCreator{}, &fakeCompleter{}, malformed, &fakeLister{}, &fakeGetter{}, &fakeDashboard{})
	recorder = serve(t, handler, allowAuth, http.MethodPatch, "/api/v1/todos/todo-1", `{"version":1,"dueAtUtc":"bad"}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("malformed due status = %d, want 422", recorder.Code)
	}
}

func TestListTodosParsesFiltersAndRejectsBadValues(t *testing.T) {
	lister := &fakeLister{result: []tododto.Todo{{ID: "todo-1", Title: "提交周报", Status: "pending", Version: 1}}}
	handler := newTestHandler(&fakeCreator{}, &fakeCompleter{}, &fakeUpdater{}, lister, &fakeGetter{}, &fakeDashboard{})

	recorder := serve(t, handler, allowAuth, http.MethodGet,
		"/api/v1/todos?keyword=%E5%91%A8%E6%8A%A5&status=pending&dueFrom=2026-08-18T00:00:00Z&dueTo=2026-08-19T00:00:00Z&noDue=true", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Todos []map[string]any `json:"todos"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if len(body.Todos) != 1 || body.Todos[0]["id"] != "todo-1" {
		t.Fatalf("body = %#v", body)
	}
	filters := lister.filters
	if filters.Keyword != "周报" || filters.Status != "pending" || !filters.NoDue {
		t.Fatalf("filters = %#v", filters)
	}
	if filters.DueFrom == nil || !filters.DueFrom.Equal(time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("dueFrom = %v", filters.DueFrom)
	}
	if filters.DueTo == nil || !filters.DueTo.Equal(time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("dueTo = %v", filters.DueTo)
	}
	if lister.workspaceID != "ws-1" || lister.ownerUserID != "user-1" {
		t.Fatalf("lister scope = %s/%s", lister.workspaceID, lister.ownerUserID)
	}

	recorder = serve(t, handler, allowAuth, http.MethodGet, "/api/v1/todos?dueFrom=bad-time", "")
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad dueFrom status = %d, want 422", recorder.Code)
	}
	recorder = serve(t, handler, allowAuth, http.MethodGet, "/api/v1/todos?status=bogus", "")
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad status param = %d, want 422", recorder.Code)
	}
}

func TestDashboardRequiresValidTimezone(t *testing.T) {
	dashboard := &fakeDashboard{result: tododto.DashboardSummary{
		PendingTotal: 2, DueToday: 1, CheckedAt: time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC),
	}}
	handler := newTestHandler(&fakeCreator{}, &fakeCompleter{}, &fakeUpdater{}, &fakeLister{}, &fakeGetter{}, dashboard)

	recorder := serve(t, handler, allowAuth, http.MethodGet, "/api/v1/dashboard/summary?timezone=Asia%2FShanghai", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if body["pendingTotal"] != float64(2) || body["reminderRetrying"] != float64(0) || body["reminderFailed"] != float64(0) {
		t.Fatalf("body = %#v", body)
	}
	if _, ok := body["checkedAt"]; !ok {
		t.Fatalf("body missing checkedAt: %#v", body)
	}
	if dashboard.timezone != "Asia/Shanghai" || dashboard.workspaceID != "ws-1" {
		t.Fatalf("dashboard args = %#v", dashboard)
	}

	recorder = serve(t, handler, allowAuth, http.MethodGet, "/api/v1/dashboard/summary", "")
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing timezone status = %d, want 422", recorder.Code)
	}

	invalid := &fakeDashboard{err: domain.ErrInvalidTimezone}
	handler = newTestHandler(&fakeCreator{}, &fakeCompleter{}, &fakeUpdater{}, &fakeLister{}, &fakeGetter{}, invalid)
	recorder = serve(t, handler, allowAuth, http.MethodGet, "/api/v1/dashboard/summary?timezone=Not%2FZone", "")
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid timezone status = %d, want 422", recorder.Code)
	}
}

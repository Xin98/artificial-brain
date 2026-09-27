package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
	identitydto "github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/platform/observability"
)

var testPrincipal = identitydto.Principal{UserID: "user-1", WorkspaceID: "ws-1", SessionID: "session-1"}
var testNow = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

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

type fakeProcessor struct {
	workspaceID string
	userID      string
	sessionID   string
	text        string
	timezone    string
	response    dto.MessageResponse
	err         error
}

func (p *fakeProcessor) Handle(_ context.Context, workspaceID, userID, sessionID, text, timezone string) (dto.MessageResponse, error) {
	p.workspaceID, p.userID, p.sessionID, p.text, p.timezone = workspaceID, userID, sessionID, text, timezone
	return p.response, p.err
}

type fakeSessionLister struct {
	view dto.SessionListView
	err  error
}

func (l *fakeSessionLister) Handle(_ context.Context, _, _ string) (dto.SessionListView, error) {
	return l.view, l.err
}

type fakeSessionCreator struct {
	title string
	view  dto.SessionView
	err   error
}

func (c *fakeSessionCreator) Handle(_ context.Context, _, _, title string) (dto.SessionView, error) {
	c.title = title
	return c.view, c.err
}

type fakeSessionRenamer struct {
	sessionID string
	title     string
	view      dto.SessionView
	err       error
}

func (rn *fakeSessionRenamer) Handle(_ context.Context, _, _, sessionID, title string) (dto.SessionView, error) {
	rn.sessionID, rn.title = sessionID, title
	return rn.view, rn.err
}

type fakeSessionDeleter struct {
	sessionID string
	err       error
}

func (d *fakeSessionDeleter) Handle(_ context.Context, _, _, sessionID string) error {
	d.sessionID = sessionID
	return d.err
}

type fakeHistory struct {
	sessionID string
	view      dto.SessionHistoryView
	err       error
}

func (g *fakeHistory) Handle(_ context.Context, _, _, sessionID string) (dto.SessionHistoryView, error) {
	g.sessionID = sessionID
	return g.view, g.err
}

func newTestHandler(processor *fakeProcessor) *Handler {
	return &Handler{
		ProcessMessage:     processor,
		CreateConfirmation: &fakeConfirmationCreator{},
		ConfirmAction:      &fakeConfirmer{},
		ListSessions:       &fakeSessionLister{},
		CreateSession:      &fakeSessionCreator{},
		RenameSession:      &fakeSessionRenamer{},
		DeleteSession:      &fakeSessionDeleter{},
		GetHistory:         &fakeHistory{},
	}
}

type fakeConfirmationCreator struct {
	intent       string
	todoID       string
	confirmation domain.ConfirmationRequest
	err          error
}

func (c *fakeConfirmationCreator) Handle(_ context.Context, _, _, intent, todoID string) (domain.ConfirmationRequest, error) {
	c.intent, c.todoID = intent, todoID
	return c.confirmation, c.err
}

type fakeConfirmer struct {
	confirmationID string
	response       dto.MessageResponse
	err            error
}

func (c *fakeConfirmer) Handle(_ context.Context, _, _, confirmationID string) (dto.MessageResponse, error) {
	c.confirmationID = confirmationID
	return c.response, c.err
}

func serve(t *testing.T, h *Handler, auth func(http.Handler) http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	RegisterRoutes(mux, auth, h)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	return recorder
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode body error = %v, body = %s", err, recorder.Body.String())
	}
	return body
}

func TestConversationRoutesRequireAuthentication(t *testing.T) {
	handler := newTestHandler(&fakeProcessor{})
	routes := []struct {
		method string
		target string
		body   string
	}{
		{http.MethodPost, "/api/v1/conversation/messages", `{"text":"你好","timezone":"UTC"}`},
		{http.MethodGet, "/api/v1/conversation/sessions", ""},
		{http.MethodPost, "/api/v1/conversation/sessions", `{"title":"周报"}`},
		{http.MethodGet, "/api/v1/conversation/sessions/session-1/messages", ""},
		{http.MethodPatch, "/api/v1/conversation/sessions/session-1", `{"title":"新标题"}`},
		{http.MethodDelete, "/api/v1/conversation/sessions/session-1", ""},
		{http.MethodPost, "/api/v1/confirmations", `{"intent":"todo.delete","todoId":"todo-1"}`},
		{http.MethodPost, "/api/v1/confirmations/conf-1/confirm", ""},
	}
	for _, route := range routes {
		recorder := serve(t, handler, denyAuth, route.method, route.target, route.body)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401", route.method, route.target, recorder.Code)
		}
	}
}

func TestMessagesReturnsKindAndCorrelationID(t *testing.T) {
	processor := &fakeProcessor{response: dto.MessageResponse{
		Kind: dto.KindTodoCreated, SessionID: "session-9", Reply: "好的。",
	}}
	handler := newTestHandler(processor)

	mux := http.NewServeMux()
	RegisterRoutes(mux, allowAuth, handler)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversation/messages",
		strings.NewReader(`{"text":"明天提醒我提交周报","timezone":"Asia/Shanghai","sessionId":"session-9"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(observability.WithCorrelationID(req.Context(), "corr-123"))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	body := decodeBody(t, recorder)
	if body["kind"] != string(dto.KindTodoCreated) {
		t.Fatalf("kind = %v", body["kind"])
	}
	if body["correlationId"] != "corr-123" {
		t.Fatalf("correlationId = %v, want corr-123", body["correlationId"])
	}
	if body["sessionId"] != "session-9" || body["reply"] != "好的。" {
		t.Fatalf("session fields = %#v", body)
	}
	if processor.workspaceID != "ws-1" || processor.userID != "user-1" || processor.sessionID != "session-9" ||
		processor.text != "明天提醒我提交周报" || processor.timezone != "Asia/Shanghai" {
		t.Fatalf("processor args = %#v", processor)
	}
}

func TestMessagesMapsUnknownSessionToNotFound(t *testing.T) {
	processor := &fakeProcessor{err: domain.ErrSessionNotFound}
	handler := newTestHandler(processor)

	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/conversation/messages",
		`{"text":"你好","timezone":"UTC","sessionId":"session-x"}`)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if decoded := decodeBody(t, recorder); decoded["code"] != "session_not_found" {
		t.Fatalf("envelope = %#v", decoded)
	}
}

func TestMessagesRejectsInvalidTurns(t *testing.T) {
	handler := &Handler{ProcessMessage: &fakeProcessor{}, CreateConfirmation: &fakeConfirmationCreator{}, ConfirmAction: &fakeConfirmer{}}
	cases := map[string]string{
		"missing text":     `{"timezone":"UTC"}`,
		"empty text":       `{"text":"","timezone":"UTC"}`,
		"missing timezone": `{"text":"你好"}`,
		"unknown field":    `{"text":"你好","timezone":"UTC","bogus":1}`,
		"oversized text":   `{"text":"` + strings.Repeat("字", 1001) + `","timezone":"UTC"}`,
	}
	for name, body := range cases {
		recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/conversation/messages", body)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s status = %d, want 422", name, recorder.Code)
		}
		if decoded := decodeBody(t, recorder); decoded["code"] != "validation_error" {
			t.Fatalf("%s envelope = %#v", name, decoded)
		}
	}
}

func TestMessagesMapsProcessorErrorToInternal(t *testing.T) {
	processor := &fakeProcessor{err: errors.New("model down")}
	handler := &Handler{ProcessMessage: processor, CreateConfirmation: &fakeConfirmationCreator{}, ConfirmAction: &fakeConfirmer{}}

	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/conversation/messages", `{"text":"你好","timezone":"UTC"}`)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if decoded := decodeBody(t, recorder); decoded["code"] != "internal_error" {
		t.Fatalf("envelope = %#v", decoded)
	}
}

func TestCreateConfirmationReturns201(t *testing.T) {
	creator := &fakeConfirmationCreator{confirmation: domain.ConfirmationRequest{
		ID: "conf-1", ExpiresAt: testNow.Add(5 * time.Minute),
	}}
	handler := &Handler{ProcessMessage: &fakeProcessor{}, CreateConfirmation: creator, ConfirmAction: &fakeConfirmer{}}

	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/confirmations", `{"intent":"todo.delete","todoId":"todo-1"}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body = %s", recorder.Code, recorder.Body.String())
	}
	body := decodeBody(t, recorder)
	if body["confirmationId"] != "conf-1" {
		t.Fatalf("confirmationId = %v", body["confirmationId"])
	}
	if body["expiresAt"] != testNow.Add(5*time.Minute).UTC().Format(time.RFC3339) {
		t.Fatalf("expiresAt = %v", body["expiresAt"])
	}
	if creator.intent != "todo.delete" || creator.todoID != "todo-1" {
		t.Fatalf("creator args = %#v", creator)
	}
}

func TestCreateConfirmationErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		err    error
		status int
		code   string
	}{
		{"missing todoId", `{"intent":"todo.delete"}`, nil, http.StatusUnprocessableEntity, "validation_error"},
		{"non-delete intent", `{"intent":"todo.create","todoId":"todo-1"}`, nil, http.StatusUnprocessableEntity, "validation_error"},
		{"todo not found", `{"intent":"todo.delete","todoId":"todo-1"}`, domain.ErrTodoNotFound, http.StatusNotFound, "not_found"},
		{"todo not pending", `{"intent":"todo.delete","todoId":"todo-1"}`, domain.ErrTodoNotPending, http.StatusConflict, "conflict"},
		{"unsupported by domain", `{"intent":"todo.delete","todoId":"todo-1"}`, domain.ErrUnsupportedConfirmationIntent, http.StatusUnprocessableEntity, "validation_error"},
	}
	for _, tc := range cases {
		creator := &fakeConfirmationCreator{err: tc.err}
		handler := &Handler{ProcessMessage: &fakeProcessor{}, CreateConfirmation: creator, ConfirmAction: &fakeConfirmer{}}
		recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/confirmations", tc.body)
		if recorder.Code != tc.status {
			t.Fatalf("%s status = %d, want %d", tc.name, recorder.Code, tc.status)
		}
		if decoded := decodeBody(t, recorder); decoded["code"] != tc.code {
			t.Fatalf("%s envelope = %#v, want %s", tc.name, decoded, tc.code)
		}
	}
}

func TestConfirmActionReturnsDeletedKind(t *testing.T) {
	confirmer := &fakeConfirmer{response: dto.MessageResponse{Kind: dto.KindTodoDeleted, TodoID: "todo-1"}}
	handler := &Handler{ProcessMessage: &fakeProcessor{}, CreateConfirmation: &fakeConfirmationCreator{}, ConfirmAction: confirmer}

	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/confirmations/conf-1/confirm", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	body := decodeBody(t, recorder)
	if body["kind"] != string(dto.KindTodoDeleted) || body["todoId"] != "todo-1" {
		t.Fatalf("body = %#v", body)
	}
	if confirmer.confirmationID != "conf-1" {
		t.Fatalf("confirmer id = %q", confirmer.confirmationID)
	}
}

func TestConfirmActionErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"not found", domain.ErrConfirmationNotFound, http.StatusNotFound, "not_found"},
		{"consumed", domain.ErrConfirmationConsumed, http.StatusConflict, "conflict"},
		{"stale todo version", domain.ErrConfirmationTodoVersionStale, http.StatusConflict, "conflict"},
		{"expired", domain.ErrConfirmationExpired, http.StatusGone, "confirmation_expired"},
		{"todo vanished", domain.ErrTodoNotFound, http.StatusNotFound, "not_found"},
	}
	for _, tc := range cases {
		confirmer := &fakeConfirmer{err: tc.err}
		handler := &Handler{ProcessMessage: &fakeProcessor{}, CreateConfirmation: &fakeConfirmationCreator{}, ConfirmAction: confirmer}
		recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/confirmations/conf-1/confirm", "")
		if recorder.Code != tc.status {
			t.Fatalf("%s status = %d, want %d", tc.name, recorder.Code, tc.status)
		}
		if decoded := decodeBody(t, recorder); decoded["code"] != tc.code {
			t.Fatalf("%s envelope = %#v, want %s", tc.name, decoded, tc.code)
		}
	}
}

func TestListSessionsReturns200(t *testing.T) {
	lister := &fakeSessionLister{view: dto.SessionListView{Sessions: []dto.SessionView{
		{ID: "session-1", Title: "周报", CreatedAt: testNow, UpdatedAt: testNow},
	}}}
	handler := newTestHandler(&fakeProcessor{})
	handler.ListSessions = lister

	recorder := serve(t, handler, allowAuth, http.MethodGet, "/api/v1/conversation/sessions", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	body := decodeBody(t, recorder)
	sessions, ok := body["sessions"].([]any)
	if !ok || len(sessions) != 1 || sessions[0].(map[string]any)["id"] != "session-1" {
		t.Fatalf("body = %#v", body)
	}
}

func TestCreateSessionReturns201(t *testing.T) {
	creator := &fakeSessionCreator{view: dto.SessionView{ID: "session-1", Title: "周报", CreatedAt: testNow, UpdatedAt: testNow}}
	handler := newTestHandler(&fakeProcessor{})
	handler.CreateSession = creator

	recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/conversation/sessions", `{"title":"周报"}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body = %s", recorder.Code, recorder.Body.String())
	}
	body := decodeBody(t, recorder)
	if body["id"] != "session-1" || body["title"] != "周报" {
		t.Fatalf("body = %#v", body)
	}
	if creator.title != "周报" {
		t.Fatalf("creator title = %q", creator.title)
	}

	// An absent body falls back to the default title.
	recorder = serve(t, handler, allowAuth, http.MethodPost, "/api/v1/conversation/sessions", "")
	if recorder.Code != http.StatusCreated {
		t.Fatalf("empty body status = %d, want 201", recorder.Code)
	}
	if creator.title != "" {
		t.Fatalf("creator title = %q, want blank for the domain fallback", creator.title)
	}
}

func TestCreateSessionRejectsInvalidBodies(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		creatorErr error
	}{
		{"invalid title", `{"title":"` + strings.Repeat("长", 51) + `"}`, domain.ErrSessionTitleInvalid},
		{"unknown field", `{"title":"周报","bogus":1}`, nil},
		{"malformed json", `{"title":`, nil},
	}
	for _, tc := range cases {
		handler := newTestHandler(&fakeProcessor{})
		handler.CreateSession = &fakeSessionCreator{err: tc.creatorErr}
		recorder := serve(t, handler, allowAuth, http.MethodPost, "/api/v1/conversation/sessions", tc.body)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s status = %d, want 422, body = %s", tc.name, recorder.Code, recorder.Body.String())
		}
		if decoded := decodeBody(t, recorder); decoded["code"] != "validation_error" {
			t.Fatalf("%s envelope = %#v", tc.name, decoded)
		}
	}
}

func TestRenameSessionMapping(t *testing.T) {
	renamer := &fakeSessionRenamer{view: dto.SessionView{ID: "session-9", Title: "新标题", CreatedAt: testNow, UpdatedAt: testNow}}
	handler := newTestHandler(&fakeProcessor{})
	handler.RenameSession = renamer

	recorder := serve(t, handler, allowAuth, http.MethodPatch, "/api/v1/conversation/sessions/session-9", `{"title":"新标题"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	if body := decodeBody(t, recorder); body["title"] != "新标题" || body["id"] != "session-9" {
		t.Fatalf("body = %#v", body)
	}
	if renamer.sessionID != "session-9" || renamer.title != "新标题" {
		t.Fatalf("renamer args = %#v", renamer)
	}

	handler.RenameSession = &fakeSessionRenamer{err: domain.ErrSessionNotFound}
	recorder = serve(t, handler, allowAuth, http.MethodPatch, "/api/v1/conversation/sessions/session-x", `{"title":"任意"}`)
	if recorder.Code != http.StatusNotFound || decodeBody(t, recorder)["code"] != "session_not_found" {
		t.Fatalf("unknown session = %d %s", recorder.Code, recorder.Body.String())
	}

	handler.RenameSession = &fakeSessionRenamer{err: domain.ErrSessionTitleInvalid}
	recorder = serve(t, handler, allowAuth, http.MethodPatch, "/api/v1/conversation/sessions/session-9", `{"title":""}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid title status = %d, want 422", recorder.Code)
	}

	recorder = serve(t, handler, allowAuth, http.MethodPatch, "/api/v1/conversation/sessions/session-9", `{"bogus":1}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown field status = %d, want 422", recorder.Code)
	}
}

func TestDeleteSessionMapping(t *testing.T) {
	deleter := &fakeSessionDeleter{}
	handler := newTestHandler(&fakeProcessor{})
	handler.DeleteSession = deleter

	recorder := serve(t, handler, allowAuth, http.MethodDelete, "/api/v1/conversation/sessions/session-9", "")
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	if deleter.sessionID != "session-9" {
		t.Fatalf("deleter id = %q", deleter.sessionID)
	}

	handler.DeleteSession = &fakeSessionDeleter{err: domain.ErrSessionNotFound}
	recorder = serve(t, handler, allowAuth, http.MethodDelete, "/api/v1/conversation/sessions/session-x", "")
	if recorder.Code != http.StatusNotFound || decodeBody(t, recorder)["code"] != "session_not_found" {
		t.Fatalf("unknown session = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestSessionMessagesReplaysHistory(t *testing.T) {
	chat := "chat"
	getter := &fakeHistory{view: dto.SessionHistoryView{
		SessionID: "session-9",
		Title:     "周报",
		Messages: []dto.MessageView{
			{ID: "1", Role: "user", Body: "你好", ResolvedIntent: &chat, CreatedAt: testNow},
			{ID: "2", Role: "assistant", Body: "你好！有什么可以帮你？", CreatedAt: testNow},
		},
	}}
	handler := newTestHandler(&fakeProcessor{})
	handler.GetHistory = getter

	recorder := serve(t, handler, allowAuth, http.MethodGet, "/api/v1/conversation/sessions/session-9/messages", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	body := decodeBody(t, recorder)
	if body["sessionId"] != "session-9" || body["title"] != "周报" {
		t.Fatalf("body = %#v", body)
	}
	messages := body["messages"].([]any)
	if len(messages) != 2 || messages[1].(map[string]any)["role"] != "assistant" {
		t.Fatalf("messages = %#v", messages)
	}
	if getter.sessionID != "session-9" {
		t.Fatalf("getter id = %q", getter.sessionID)
	}

	handler.GetHistory = &fakeHistory{err: domain.ErrSessionNotFound}
	recorder = serve(t, handler, allowAuth, http.MethodGet, "/api/v1/conversation/sessions/session-x/messages", "")
	if recorder.Code != http.StatusNotFound || decodeBody(t, recorder)["code"] != "session_not_found" {
		t.Fatalf("unknown session = %d %s", recorder.Code, recorder.Body.String())
	}
}

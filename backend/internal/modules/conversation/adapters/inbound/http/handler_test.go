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
	text        string
	timezone    string
	response    dto.MessageResponse
	err         error
}

func (p *fakeProcessor) Handle(_ context.Context, workspaceID, userID, text, timezone string) (dto.MessageResponse, error) {
	p.workspaceID, p.userID, p.text, p.timezone = workspaceID, userID, text, timezone
	return p.response, p.err
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
	handler := &Handler{
		ProcessMessage:     &fakeProcessor{},
		CreateConfirmation: &fakeConfirmationCreator{},
		ConfirmAction:      &fakeConfirmer{},
	}
	routes := []struct {
		method string
		target string
		body   string
	}{
		{http.MethodPost, "/api/v1/conversation/messages", `{"text":"你好","timezone":"UTC"}`},
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
	processor := &fakeProcessor{response: dto.MessageResponse{Kind: dto.KindTodoCreated}}
	handler := &Handler{ProcessMessage: processor, CreateConfirmation: &fakeConfirmationCreator{}, ConfirmAction: &fakeConfirmer{}}

	mux := http.NewServeMux()
	RegisterRoutes(mux, allowAuth, handler)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversation/messages",
		strings.NewReader(`{"text":"明天提醒我提交周报","timezone":"Asia/Shanghai"}`))
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
	if processor.workspaceID != "ws-1" || processor.userID != "user-1" ||
		processor.text != "明天提醒我提交周报" || processor.timezone != "Asia/Shanghai" {
		t.Fatalf("processor args = %#v", processor)
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

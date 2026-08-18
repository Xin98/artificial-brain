// Package http serves the todo and dashboard routes. The principal arrives
// on the context via Identity's session middleware; this package never
// touches cookies or tokens.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	identitydto "github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	tododto "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

// Narrow application seams consumed by the HTTP handlers. The concrete
// command/query handlers satisfy these interfaces.
type (
	todoCreator interface {
		Handle(ctx context.Context, request tododto.CreateTodoRequest) (tododto.Todo, error)
	}
	todoCompleter interface {
		Handle(ctx context.Context, request tododto.CompleteTodoRequest) (tododto.Todo, error)
	}
	todoUpdater interface {
		Handle(ctx context.Context, request tododto.UpdateTodoRequest) (tododto.Todo, error)
	}
	todoLister interface {
		Handle(ctx context.Context, workspaceID, ownerUserID string, filters tododto.ListFilters) ([]tododto.Todo, error)
	}
	todoGetter interface {
		Handle(ctx context.Context, workspaceID, ownerUserID, todoID string) (tododto.Todo, error)
	}
	dashboardSummaryProvider interface {
		Handle(ctx context.Context, workspaceID, ownerUserID, timezone string) (tododto.DashboardSummary, error)
	}
)

// Handler serves the todo and dashboard HTTP routes.
type Handler struct {
	Create    todoCreator
	Complete  todoCompleter
	Update    todoUpdater
	List      todoLister
	Get       todoGetter
	Dashboard dashboardSummaryProvider
}

// RegisterRoutes registers the todo and dashboard routes on mux, all wrapped
// with the auth middleware.
func RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler, h *Handler) {
	mux.Handle("GET /api/v1/todos", auth(http.HandlerFunc(h.list)))
	mux.Handle("POST /api/v1/todos", auth(http.HandlerFunc(h.create)))
	mux.Handle("GET /api/v1/todos/{todoId}", auth(http.HandlerFunc(h.get)))
	mux.Handle("PATCH /api/v1/todos/{todoId}", auth(http.HandlerFunc(h.update)))
	mux.Handle("POST /api/v1/todos/{todoId}/complete", auth(http.HandlerFunc(h.complete)))
	mux.Handle("GET /api/v1/dashboard/summary", auth(http.HandlerFunc(h.dashboard)))
}

func principalFrom(w http.ResponseWriter, r *http.Request) (identitydto.Principal, bool) {
	principal, ok := identitydto.PrincipalFromContext(r.Context())
	if !ok {
		writeUnauthenticated(w, r)
		return identitydto.Principal{}, false
	}
	return principal, true
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	var body struct {
		Title           string  `json:"title"`
		Description     *string `json:"description"`
		DueAtUTC        *string `json:"dueAtUtc"`
		TimezoneAtInput *string `json:"timezoneAtInput"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	request := tododto.CreateTodoRequest{
		WorkspaceID:     principal.WorkspaceID,
		UserID:          principal.UserID,
		Title:           body.Title,
		Description:     body.Description,
		TimezoneAtInput: body.TimezoneAtInput,
	}
	if body.DueAtUTC != nil {
		due, err := time.Parse(time.RFC3339, *body.DueAtUTC)
		if err != nil {
			writeValidationError(w, r)
			return
		}
		request.DueAtUTC = &due
	}
	todo, err := h.Create.Handle(r.Context(), request)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidTitle) {
			writeValidationError(w, r)
			return
		}
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusCreated, todo)
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	var body struct {
		Version *int `json:"version"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Version == nil {
		writeValidationError(w, r)
		return
	}
	todo, err := h.Complete.Handle(r.Context(), tododto.CompleteTodoRequest{
		WorkspaceID: principal.WorkspaceID,
		UserID:      principal.UserID,
		TodoID:      r.PathValue("todoId"),
		Version:     *body.Version,
	})
	if err != nil {
		writeTodoError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, todo)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	todo, err := h.Get.Handle(r.Context(), principal.WorkspaceID, principal.UserID, r.PathValue("todoId"))
	if err != nil {
		writeTodoError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, todo)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	var body struct {
		Version         *int            `json:"version"`
		Title           *string         `json:"title"`
		Description     *string         `json:"description"`
		DueAtUTC        json.RawMessage `json:"dueAtUtc"`
		TimezoneAtInput *string         `json:"timezoneAtInput"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Version == nil {
		writeValidationError(w, r)
		return
	}
	// A present dueAtUtc (even null) means the caller is setting the due
	// field; a null clears it.
	request := tododto.UpdateTodoRequest{
		WorkspaceID:     principal.WorkspaceID,
		UserID:          principal.UserID,
		TodoID:          r.PathValue("todoId"),
		Version:         *body.Version,
		Title:           body.Title,
		Description:     body.Description,
		TimezoneAtInput: body.TimezoneAtInput,
		DueChanged:      len(body.DueAtUTC) > 0,
	}
	if len(body.DueAtUTC) > 0 && string(body.DueAtUTC) != "null" {
		var raw string
		if err := json.Unmarshal(body.DueAtUTC, &raw); err != nil {
			writeValidationError(w, r)
			return
		}
		due, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeValidationError(w, r)
			return
		}
		request.DueAtUTC = &due
	}
	todo, err := h.Update.Handle(r.Context(), request)
	if err != nil {
		writeTodoError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, todo)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	filters := tododto.ListFilters{
		Keyword: query.Get("keyword"),
		Status:  query.Get("status"),
		NoDue:   query.Get("noDue") == "true",
	}
	if filters.Status != "" && filters.Status != string(domain.StatusPending) && filters.Status != string(domain.StatusCompleted) {
		writeValidationError(w, r)
		return
	}
	for name, target := range map[string]**time.Time{
		"dueFrom": &filters.DueFrom,
		"dueTo":   &filters.DueTo,
	} {
		if value := query.Get(name); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				writeValidationError(w, r)
				return
			}
			*target = &parsed
		}
	}
	todos, err := h.List.Handle(r.Context(), principal.WorkspaceID, principal.UserID, filters)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	if todos == nil {
		todos = []tododto.Todo{}
	}
	writeJSON(w, http.StatusOK, map[string][]tododto.Todo{"todos": todos})
}

// writeTodoError maps todo domain errors to stable envelopes.
func writeTodoError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrTodoNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "todo not found")
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrTodoDeleted), errors.Is(err, domain.ErrAlreadyCompleted):
		writeError(w, r, http.StatusConflict, "conflict", "todo state conflict")
	case errors.Is(err, domain.ErrInvalidTitle):
		writeValidationError(w, r)
	default:
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

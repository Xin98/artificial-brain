// Package http serves the conversation and confirmation routes. The
// principal arrives on the context via Identity's session middleware.
package http

import (
	"context"
	"errors"
	"net/http"
	"unicode/utf8"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
	identitydto "github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/platform/observability"
)

// maxMessageLength bounds one conversation turn in characters.
const maxMessageLength = 1000

// Narrow application seams consumed by the HTTP handlers.
type (
	processMessenger interface {
		Handle(ctx context.Context, workspaceID, userID, sessionID, text, timezone string) (dto.MessageResponse, error)
	}
	confirmationCreator interface {
		Handle(ctx context.Context, workspaceID, userID, intent, todoID string) (domain.ConfirmationRequest, error)
	}
	confirmationConfirmer interface {
		Handle(ctx context.Context, workspaceID, userID, confirmationID string) (dto.MessageResponse, error)
	}
	sessionLister interface {
		Handle(ctx context.Context, workspaceID, userID string) (dto.SessionListView, error)
	}
	sessionCreator interface {
		Handle(ctx context.Context, workspaceID, userID, title string) (dto.SessionView, error)
	}
	sessionRenamer interface {
		Handle(ctx context.Context, workspaceID, userID, sessionID, title string) (dto.SessionView, error)
	}
	sessionDeleter interface {
		Handle(ctx context.Context, workspaceID, userID, sessionID string) error
	}
	historyGetter interface {
		Handle(ctx context.Context, workspaceID, userID, sessionID string) (dto.SessionHistoryView, error)
	}
)

// Handler serves the conversation HTTP routes.
type Handler struct {
	ProcessMessage     processMessenger
	CreateConfirmation confirmationCreator
	ConfirmAction      confirmationConfirmer
	ListSessions       sessionLister
	CreateSession      sessionCreator
	RenameSession      sessionRenamer
	DeleteSession      sessionDeleter
	GetHistory         historyGetter
}

// RegisterRoutes registers the conversation routes on mux, all wrapped with
// the auth middleware.
func RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler, h *Handler) {
	mux.Handle("POST /api/v1/conversation/messages", auth(http.HandlerFunc(h.messages)))
	mux.Handle("GET /api/v1/conversation/sessions", auth(http.HandlerFunc(h.listSessions)))
	mux.Handle("POST /api/v1/conversation/sessions", auth(http.HandlerFunc(h.createSession)))
	mux.Handle("GET /api/v1/conversation/sessions/{sessionId}/messages", auth(http.HandlerFunc(h.sessionMessages)))
	mux.Handle("PATCH /api/v1/conversation/sessions/{sessionId}", auth(http.HandlerFunc(h.renameSession)))
	mux.Handle("DELETE /api/v1/conversation/sessions/{sessionId}", auth(http.HandlerFunc(h.deleteSession)))
	mux.Handle("POST /api/v1/confirmations", auth(http.HandlerFunc(h.createConfirmation)))
	mux.Handle("POST /api/v1/confirmations/{confirmationId}/confirm", auth(http.HandlerFunc(h.confirm)))
}

func principalFrom(w http.ResponseWriter, r *http.Request) (identitydto.Principal, bool) {
	principal, ok := identitydto.PrincipalFromContext(r.Context())
	if !ok {
		writeUnauthenticated(w, r)
		return identitydto.Principal{}, false
	}
	return principal, true
}

// messageEnvelope flattens the application response and adds the
// correlation id required by the route contract.
type messageEnvelope struct {
	dto.MessageResponse
	CorrelationID string `json:"correlationId"`
}

func writeMessageResponse(w http.ResponseWriter, r *http.Request, response dto.MessageResponse) {
	writeJSON(w, http.StatusOK, messageEnvelope{
		MessageResponse: response,
		CorrelationID:   observability.CorrelationID(r.Context()),
	})
}

// writeSessionNotFound maps the scoped session miss to its route contract.
func writeSessionNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusNotFound, "session_not_found", "session not found")
}

func (h *Handler) messages(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	var body struct {
		Text      string `json:"text"`
		Timezone  string `json:"timezone"`
		SessionID string `json:"sessionId"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Text == "" || utf8.RuneCountInString(body.Text) > maxMessageLength || body.Timezone == "" {
		writeValidationError(w, r)
		return
	}
	response, err := h.ProcessMessage.Handle(r.Context(), principal.WorkspaceID, principal.UserID, body.SessionID, body.Text, body.Timezone)
	if err != nil {
		if errors.Is(err, domain.ErrSessionNotFound) {
			writeSessionNotFound(w, r)
			return
		}
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeMessageResponse(w, r, response)
}

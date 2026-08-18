// Package http serves the conversation and confirmation routes. The
// principal arrives on the context via Identity's session middleware.
package http

import (
	"context"
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
		Handle(ctx context.Context, workspaceID, userID, text, timezone string) (dto.MessageResponse, error)
	}
	confirmationCreator interface {
		Handle(ctx context.Context, workspaceID, userID, intent, todoID string) (domain.ConfirmationRequest, error)
	}
	confirmationConfirmer interface {
		Handle(ctx context.Context, workspaceID, userID, confirmationID string) (dto.MessageResponse, error)
	}
)

// Handler serves the conversation HTTP routes.
type Handler struct {
	ProcessMessage     processMessenger
	CreateConfirmation confirmationCreator
	ConfirmAction      confirmationConfirmer
}

// RegisterRoutes registers the conversation routes on mux, all wrapped with
// the auth middleware.
func RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler, h *Handler) {
	mux.Handle("POST /api/v1/conversation/messages", auth(http.HandlerFunc(h.messages)))
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

func (h *Handler) messages(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	var body struct {
		Text     string `json:"text"`
		Timezone string `json:"timezone"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Text == "" || utf8.RuneCountInString(body.Text) > maxMessageLength || body.Timezone == "" {
		writeValidationError(w, r)
		return
	}
	response, err := h.ProcessMessage.Handle(r.Context(), principal.WorkspaceID, principal.UserID, body.Text, body.Timezone)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeMessageResponse(w, r, response)
}

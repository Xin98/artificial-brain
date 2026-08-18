package http

import (
	"errors"
	"net/http"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
)

func (h *Handler) createConfirmation(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	var body struct {
		Intent string `json:"intent"`
		TodoID string `json:"todoId"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.TodoID == "" || body.Intent != string(domain.IntentTodoDelete) {
		writeValidationError(w, r)
		return
	}
	confirmation, err := h.CreateConfirmation.Handle(r.Context(), principal.WorkspaceID, principal.UserID, body.Intent, body.TodoID)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrTodoNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "todo not found")
		case errors.Is(err, domain.ErrTodoNotPending):
			writeError(w, r, http.StatusConflict, "conflict", "todo is not pending")
		case errors.Is(err, domain.ErrUnsupportedConfirmationIntent):
			writeValidationError(w, r)
		default:
			writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"confirmationId": confirmation.ID,
		"expiresAt":      confirmation.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (h *Handler) confirm(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	response, err := h.ConfirmAction.Handle(r.Context(), principal.WorkspaceID, principal.UserID, r.PathValue("confirmationId"))
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrConfirmationNotFound), errors.Is(err, domain.ErrTodoNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "confirmation not found")
		case errors.Is(err, domain.ErrConfirmationConsumed), errors.Is(err, domain.ErrConfirmationTodoVersionStale):
			writeError(w, r, http.StatusConflict, "conflict", "confirmation cannot be used")
		case errors.Is(err, domain.ErrConfirmationExpired):
			writeError(w, r, http.StatusGone, "confirmation_expired", "confirmation expired")
		default:
			writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}
	writeMessageResponse(w, r, response)
}

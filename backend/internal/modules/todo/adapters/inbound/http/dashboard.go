package http

import (
	"errors"
	"net/http"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
)

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	timezone := r.URL.Query().Get("timezone")
	if timezone == "" {
		writeValidationError(w, r)
		return
	}
	summary, err := h.Dashboard.Handle(r.Context(), principal.WorkspaceID, principal.UserID, timezone)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidTimezone) {
			writeValidationError(w, r)
			return
		}
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/query"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
)

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || value < 0 || value > 2147483647-query.MaxListedSessions-1 {
			writeValidationError(w, r)
			return
		}
		for _, c := range raw {
			if c < '0' || c > '9' {
				writeValidationError(w, r)
				return
			}
		}
		offset = int(value)
	}
	var view dto.SessionListView
	var err error
	if paged, ok := h.ListSessions.(interface {
		HandlePage(context.Context, string, string, int) (dto.SessionListView, error)
	}); ok {
		view, err = paged.HandlePage(r.Context(), principal.WorkspaceID, principal.UserID, offset)
	} else {
		view, err = h.ListSessions.Handle(r.Context(), principal.WorkspaceID, principal.UserID)
	}
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	if !decodeOptionalJSON(w, r, &body) {
		return
	}
	view, err := h.CreateSession.Handle(r.Context(), principal.WorkspaceID, principal.UserID, body.Title)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrSessionTitleInvalid), errors.Is(err, domain.ErrInvalidSession):
			writeValidationError(w, r)
		default:
			writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (h *Handler) renameSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	view, err := h.RenameSession.Handle(r.Context(), principal.WorkspaceID, principal.UserID, r.PathValue("sessionId"), body.Title)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrSessionNotFound):
			writeSessionNotFound(w, r)
		case errors.Is(err, domain.ErrSessionTitleInvalid):
			writeValidationError(w, r)
		default:
			writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	if err := h.DeleteSession.Handle(r.Context(), principal.WorkspaceID, principal.UserID, r.PathValue("sessionId")); err != nil {
		if errors.Is(err, domain.ErrSessionNotFound) {
			writeSessionNotFound(w, r)
			return
		}
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) sessionMessages(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	before := r.URL.Query().Get("before")
	if before != "" {
		value, err := strconv.ParseInt(before, 10, 64)
		if err != nil || value <= 0 {
			writeValidationError(w, r)
			return
		}
		for _, c := range before {
			if c < '0' || c > '9' {
				writeValidationError(w, r)
				return
			}
		}
	}
	var view dto.SessionHistoryView
	var err error
	if paged, ok := h.GetHistory.(interface {
		HandlePage(context.Context, string, string, string, string) (dto.SessionHistoryView, error)
	}); ok {
		view, err = paged.HandlePage(r.Context(), principal.WorkspaceID, principal.UserID, r.PathValue("sessionId"), before)
	} else {
		view, err = h.GetHistory.Handle(r.Context(), principal.WorkspaceID, principal.UserID, r.PathValue("sessionId"))
	}
	if err != nil {
		if errors.Is(err, domain.ErrSessionNotFound) {
			writeSessionNotFound(w, r)
			return
		}
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

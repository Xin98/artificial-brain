package http

import (
	"encoding/json"
	"net/http"

	"github.com/Xin98/artificial-brain/backend/internal/platform/observability"
)

// SessionCookieName is the bearer cookie for authenticated sessions.
const SessionCookieName = "ab_session"

type errorResponse struct {
	Code          string `json:"code"`
	Message       string `json:"message"`
	CorrelationID string `json:"correlationId"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, errorResponse{
		Code:          code,
		Message:       message,
		CorrelationID: observability.CorrelationID(r.Context()),
	})
}

func writeValidationError(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "request is invalid")
}

func writeUnauthenticated(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusUnauthorized, "unauthenticated", "authentication is required")
}

// decodeJSON decodes the request body into dst, rejecting unknown fields.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeValidationError(w, r)
		return false
	}
	return true
}

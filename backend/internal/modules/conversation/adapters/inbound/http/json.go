package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/Xin98/artificial-brain/backend/internal/platform/observability"
)

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

// decodeOptionalJSON is decodeJSON for routes whose whole body is optional:
// an absent or blank body decodes as the zero value, while a present body is
// still strict about unknown fields.
func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeValidationError(w, r)
		return false
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeValidationError(w, r)
		return false
	}
	return true
}

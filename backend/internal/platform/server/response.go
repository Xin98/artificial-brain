package server

import (
	"encoding/json"
	"net/http"

	"github.com/Xin98/artificial-brain/backend/internal/platform/observability"
)

type ErrorResponse struct {
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
	writeJSON(w, status, ErrorResponse{Code: code, Message: message, CorrelationID: observability.CorrelationID(r.Context())})
}

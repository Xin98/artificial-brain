package server

import (
	"context"
	"net/http"

	"github.com/Xin98/artificial-brain/backend/internal/platform/systemhealth"
)

type Readiness func(context.Context) error

func NewAPIHandler(ready Readiness, checker *systemhealth.Checker) http.Handler {
	return Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}

		switch r.URL.Path {
		case "/health/live":
			writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
		case "/health/ready":
			if err := ready(r.Context()); err != nil {
				writeError(w, r, http.StatusServiceUnavailable, "not_ready", "service is not ready")
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
		case "/api/v1/system/health":
			writeJSON(w, http.StatusOK, checker.Check(r.Context()))
		default:
			writeError(w, r, http.StatusNotFound, "not_found", "resource not found")
		}
	}))
}

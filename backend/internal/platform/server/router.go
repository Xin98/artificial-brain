package server

import (
	"net/http"
	"strings"

	"github.com/Xin98/artificial-brain/backend/internal/platform/systemhealth"
)

// RegisterHealthRoutes registers the ITER-0001 health endpoints on mux using
// method+path patterns. The health semantics are identical to NewAPIHandler.
func RegisterHealthRoutes(mux *http.ServeMux, ready Readiness, checker *systemhealth.Checker) {
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := ready(r.Context()); err != nil {
			writeError(w, r, http.StatusServiceUnavailable, "not_ready", "service is not ready")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
	})
	mux.HandleFunc("GET /api/v1/system/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, checker.Check(r.Context()))
	})
}

// NewAPIRouter wraps mux so every response carries a correlation ID and the
// default not-found and method-not-allowed responses produced by http.ServeMux
// use the stable JSON error envelope. Structured application/json responses
// written by handlers pass through unchanged. Business routes are registered on
// mux before calling this.
func NewAPIRouter(mux *http.ServeMux) http.Handler {
	return Correlation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(&errorEnvelopeWriter{ResponseWriter: w, request: r}, r)
	}))
}

// errorEnvelopeWriter replaces http.ServeMux's plain-text 404 and 405 bodies
// with the JSON error envelope, preserving the Allow header on 405. Responses
// that already declare application/json are assumed to be structured handler
// output and are left untouched.
type errorEnvelopeWriter struct {
	http.ResponseWriter
	request *http.Request
	handled bool
}

func (e *errorEnvelopeWriter) WriteHeader(status int) {
	if e.handled {
		return
	}
	structured := strings.HasPrefix(e.ResponseWriter.Header().Get("Content-Type"), "application/json")
	if !structured && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) {
		e.handled = true
		if status == http.StatusMethodNotAllowed {
			writeError(e.ResponseWriter, e.request, status, "method_not_allowed", "method not allowed")
		} else {
			writeError(e.ResponseWriter, e.request, status, "not_found", "resource not found")
		}
		return
	}
	e.ResponseWriter.WriteHeader(status)
}

func (e *errorEnvelopeWriter) Write(body []byte) (int, error) {
	if e.handled {
		return len(body), nil
	}
	return e.ResponseWriter.Write(body)
}

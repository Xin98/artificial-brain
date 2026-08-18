package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestRouter(ready Readiness) http.Handler {
	mux := http.NewServeMux()
	RegisterHealthRoutes(mux, ready, healthyChecker())
	return NewAPIRouter(mux)
}

func TestRouterServesHealthRoutes(t *testing.T) {
	h := newTestRouter(func(context.Context) error { return nil })
	for _, path := range []string{"/health/live", "/health/ready", "/api/v1/system/health"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, rr.Code)
		}
		if rr.Header().Get("X-Correlation-ID") == "" {
			t.Fatalf("GET %s missing correlation ID", path)
		}
	}
}

func TestRouterReadyReturns503WhenUnready(t *testing.T) {
	h := newTestRouter(func(context.Context) error { return errors.New("postgres://secret") })
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
	var got ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Code != "not_ready" || got.CorrelationID == "" {
		t.Fatalf("body = %#v", got)
	}
}

func TestRouterUnknownPathReturnsJSONNotFound(t *testing.T) {
	h := newTestRouter(func(context.Context) error { return nil })
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(method, "/no/such/path", nil))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("%s /no/such/path status = %d, want 404", method, rr.Code)
		}
		var got ErrorResponse
		if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
			t.Fatalf("%s decode error = %v", method, err)
		}
		if got.Code != "not_found" || got.CorrelationID == "" {
			t.Fatalf("%s body = %#v", method, got)
		}
	}
}

func TestRouterMethodNotAllowedReturnsJSONWithAllow(t *testing.T) {
	h := newTestRouter(func(context.Context) error { return nil })
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/health/live", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
	if got := rr.Header().Get("Allow"); !strings.Contains(got, http.MethodGet) {
		t.Fatalf("Allow = %q, want it to include GET", got)
	}
	var got ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Code != "method_not_allowed" || got.CorrelationID == "" {
		t.Fatalf("body = %#v", got)
	}
}

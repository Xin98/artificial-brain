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

func TestWorkerHealthHandler(t *testing.T) {
	databaseUnavailable := errors.New("postgres://user:secret@example.test/app")

	for _, test := range []struct {
		name           string
		method, path   string
		ready          Readiness
		heartbeatReady bool
		wantStatus     int
		wantCode       string
		wantAllow      string
	}{
		{"live does not require database", http.MethodGet, "/health/live", func(context.Context) error { return databaseUnavailable }, false, http.StatusOK, "", ""},
		{"database unavailable", http.MethodGet, "/health/ready", func(context.Context) error { return databaseUnavailable }, true, http.StatusServiceUnavailable, "not_ready", ""},
		{"heartbeat not recorded", http.MethodGet, "/health/ready", func(context.Context) error { return nil }, false, http.StatusServiceUnavailable, "not_ready", ""},
		{"both dependencies ready", http.MethodGet, "/health/ready", func(context.Context) error { return nil }, true, http.StatusOK, "", ""},
		{"invalid method", http.MethodPost, "/health/live", func(context.Context) error { return nil }, true, http.StatusMethodNotAllowed, "method_not_allowed", http.MethodGet},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := NewWorkerHealthHandler(test.ready, func() bool { return test.heartbeatReady })
			req := httptest.NewRequest(test.method, test.path, nil)
			req.Header.Set("X-Correlation-ID", "worker-test-42")
			rr := httptest.NewRecorder()

			h.ServeHTTP(rr, req)

			if rr.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", rr.Code, test.wantStatus)
			}
			if got := rr.Header().Get("X-Correlation-ID"); got != "worker-test-42" {
				t.Fatalf("correlation ID = %q", got)
			}
			if got := rr.Header().Get("Allow"); got != test.wantAllow {
				t.Fatalf("Allow = %q, want %q", got, test.wantAllow)
			}
			if got := rr.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q", got)
			}

			if test.wantCode == "" {
				if got := rr.Body.String(); got != "{\"status\":\"healthy\"}\n" {
					t.Fatalf("body = %q", got)
				}
				return
			}

			var body ErrorResponse
			if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Code != test.wantCode || body.CorrelationID != "worker-test-42" {
				t.Fatalf("body = %#v", body)
			}
			if strings.Contains(rr.Body.String(), "secret") {
				t.Fatal("dependency error leaked")
			}
		})
	}
}

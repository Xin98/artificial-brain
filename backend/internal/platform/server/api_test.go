package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/platform/systemhealth"
	"github.com/Xin98/artificial-brain/backend/internal/platform/workerstatus"
)

func TestLiveReturnsHealthyJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	NewAPIHandler(func(context.Context) error { return errors.New("must not be called") }, healthyChecker()).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health/live", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	if got := rr.Body.String(); got != "{\"status\":\"healthy\"}\n" {
		t.Fatalf("body = %q", got)
	}
	if rr.Header().Get("X-Correlation-ID") == "" {
		t.Fatal("response correlation ID is empty")
	}
}

func TestReadyReturnsStable503(t *testing.T) {
	h := NewAPIHandler(func(context.Context) error { return errors.New("postgres://secret") }, healthyChecker())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rr.Code)
	}
	var got ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Code != "not_ready" || got.Message != "service is not ready" || got.CorrelationID == "" {
		t.Fatalf("body = %#v", got)
	}
	if strings.Contains(rr.Body.String(), "secret") {
		t.Fatal("dependency error leaked")
	}
}

func TestSystemHealthReturnsDegradedReport(t *testing.T) {
	checker := systemhealth.NewChecker(errorDB{}, fixedWorkers{}, fixedNow, 6*time.Second)
	rr := httptest.NewRecorder()
	NewAPIHandler(func(context.Context) error { return nil }, checker).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/system/health", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var got systemhealth.Report
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != systemhealth.StatusDegraded || got.Components["database"].Status != systemhealth.StatusUnavailable || got.CorrelationID == "" {
		t.Fatalf("report = %#v", got)
	}
}

func TestAPIReturnsStableJSONForMethodAndPathErrors(t *testing.T) {
	h := NewAPIHandler(func(context.Context) error { return nil }, healthyChecker())
	for _, test := range []struct {
		name, method, path, code, message string
		status                            int
		allow                             string
	}{
		{"known method", http.MethodPost, "/health/live", "method_not_allowed", "method not allowed", http.StatusMethodNotAllowed, http.MethodGet},
		{"unknown method", http.MethodPost, "/unknown", "method_not_allowed", "method not allowed", http.StatusMethodNotAllowed, http.MethodGet},
		{"unknown path", http.MethodGet, "/unknown", "not_found", "resource not found", http.StatusNotFound, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(test.method, test.path, nil))
			if rr.Code != test.status || rr.Header().Get("Allow") != test.allow || rr.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("status=%d allow=%q content-type=%q", rr.Code, rr.Header().Get("Allow"), rr.Header().Get("Content-Type"))
			}
			var got ErrorResponse
			if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Code != test.code || got.Message != test.message || got.CorrelationID == "" {
				t.Fatalf("body = %#v", got)
			}
		})
	}
}

func healthyChecker() *systemhealth.Checker {
	return systemhealth.NewChecker(healthyDB{}, healthyWorkers{}, fixedNow, 6*time.Second)
}

var fixedNow = func() time.Time { return time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC) }

type healthyDB struct{}

func (healthyDB) Ping(context.Context) error { return nil }

type errorDB struct{}

func (errorDB) Ping(context.Context) error { return errors.New("postgres://secret") }

type healthyWorkers struct{}

func (healthyWorkers) Latest(context.Context) (workerstatus.Lease, error) {
	return workerstatus.Lease{LastHeartbeatAt: fixedNow()}, nil
}

type fixedWorkers struct{}

func (fixedWorkers) Latest(context.Context) (workerstatus.Lease, error) {
	return workerstatus.Lease{}, errors.New("no worker")
}

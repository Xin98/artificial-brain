package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/Xin98/artificial-brain/backend/internal/platform/observability"
)

func TestCorrelationPassesValidHeaderToContextAndResponse(t *testing.T) {
	const want = "request_42-OK.value"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, observability.CorrelationID(r.Context()))
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Correlation-ID", want)
	rr := httptest.NewRecorder()

	Correlation(next).ServeHTTP(rr, req)

	if got := rr.Header().Get("X-Correlation-ID"); got != want {
		t.Fatalf("response correlation ID = %q, want %q", got, want)
	}
	if got := rr.Body.String(); got != want {
		t.Fatalf("context correlation ID = %q, want %q", got, want)
	}
}

func TestCorrelationGeneratesLowercaseHexForMissingHeader(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	rr := httptest.NewRecorder()

	correlationWithReader(next, bytes.NewReader(make([]byte, 16))).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := rr.Header().Get("X-Correlation-ID"); got != "00000000000000000000000000000000" {
		t.Fatalf("generated correlation ID = %q", got)
	}
}

func TestCorrelationReplacesInvalidHeader(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, observability.CorrelationID(r.Context()))
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Correlation-ID", "bad value with spaces")
	rr := httptest.NewRecorder()

	correlationWithReader(next, bytes.NewReader(bytes.Repeat([]byte{0xab}, 16))).ServeHTTP(rr, req)

	got := rr.Header().Get("X-Correlation-ID")
	if got == "bad value with spaces" || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(got) || rr.Body.String() != got {
		t.Fatalf("id = %q body = %q", got, rr.Body.String())
	}
}

func TestCorrelationReturnsStable500WhenEntropyFails(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next must not run") })
	rr := httptest.NewRecorder()

	correlationWithReader(next, failingReader{}).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	if got := rr.Body.String(); got != `{"code":"internal_error","message":"internal server error","correlationId":""}`+"\n" {
		t.Fatalf("body = %q", got)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

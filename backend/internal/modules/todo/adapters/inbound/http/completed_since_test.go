package http

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestCompletedSinceFilterValidatesAndNormalizesUTC(t *testing.T) {
	for _, value := range []string{"not-a-date", "2026-10-06"} {
		h := &Handler{List: &fakeLister{}}
		response := serve(t, h, allowAuth, http.MethodGet, "/api/v1/todos?status=completed&completedSince="+url.QueryEscape(value), "")
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%q status = %d, want 422", value, response.Code)
		}
	}
	lister := &fakeLister{}
	response := serve(t, &Handler{List: lister}, allowAuth, http.MethodGet, "/api/v1/todos?status=completed&completedSince="+url.QueryEscape("2026-10-01T08:00:00.123+08:00"), "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	got := lister.filters.CompletedSince
	if got == nil {
		t.Fatalf("completedSince not forwarded: %#v", lister.filters)
	}
	want := time.Date(2026, 10, 1, 0, 0, 0, 123000000, time.UTC)
	if !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("completedSince = %v, want normalized %v", got, want)
	}
}

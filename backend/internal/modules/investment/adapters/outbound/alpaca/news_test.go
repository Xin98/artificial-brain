package alpaca

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewsUnavailableIsNotEmptyCoverage(t *testing.T) {
	now := time.Now()
	disabled, _ := NewNews(nil, NewsConfig{})
	if _, e := disabled.Items(context.Background(), nil, now.Add(-time.Hour), now); !errors.Is(e, domain.ErrDataNotConfigured) {
		t.Fatal(e)
	}
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { count++; w.WriteHeader(401) }))
	defer srv.Close()
	news, e := NewNews(srv.Client(), NewsConfig{Enabled: true, BaseURL: srv.URL, Key: "fixture-key", Secret: "fixture-secret", Timeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = news.Items(context.Background(), nil, now.Add(-time.Hour), now); e == nil || count != 1 {
		t.Fatal(count, e)
	}
}

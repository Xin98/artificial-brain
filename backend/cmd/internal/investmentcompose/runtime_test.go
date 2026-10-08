package investmentcompose

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/platform/config"
	"net/http"
	"testing"
	"time"
)

type denyTransport struct{ calls int }

func (d *denyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	d.calls++
	return nil, errors.New("network forbidden")
}
func TestInvestmentCompositionFixtureOnly(t *testing.T) {
	old := http.DefaultTransport
	deny := &denyTransport{}
	http.DefaultTransport = deny
	defer func() { http.DefaultTransport = old }()
	now := func() time.Time { return time.Date(2026, 10, 7, 21, 0, 0, 0, time.UTC) }
	r, e := New(config.InvestmentConfig{Mode: "fixture"}, nil, now)
	if e != nil {
		t.Fatal(e)
	}
	s, e := r.Data.Read(context.Background(), "fixture", now())
	if e != nil || len(s.Bars) == 0 || r.DatasetVersion != "fixture/synthetic/v2" || deny.calls != 0 {
		t.Fatal(e, deny.calls)
	}
}

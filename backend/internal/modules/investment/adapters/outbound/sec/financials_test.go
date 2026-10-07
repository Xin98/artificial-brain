package sec

import (
	"context"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSECKeepsAccessionAndRevisionTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("no identity")
		}
		switch r.URL.Path {
		case "/submissions/CIK0000000001.json":
			fmt.Fprint(w, `{"cik":"0000000001","name":"Acme","entityType":"operating","sic":"3571","tickers":["NEW"],"exchanges":["Nasdaq"],"filings":{"recent":{"accessionNumber":["original","amended"],"filingDate":["2026-03-01","2026-04-01"],"acceptanceDateTime":["2026-03-01T15:00:00Z","2026-04-01T15:00:00Z"]},"files":[]}}`)
		case "/api/xbrl/companyfacts/CIK0000000001.json":
			fmt.Fprint(w, `{"cik":1,"facts":{"us-gaap":{"NetIncomeLoss":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":100,"accn":"original","filed":"2026-03-01"},{"start":"2025-01-01","end":"2025-12-31","val":80,"accn":"amended","filed":"2026-04-01"}]}}}}}`)
		default:
			t.Error(r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f, e := NewFinancials(srv.Client(), SECConfig{BaseURL: srv.URL, UserAgent: "Investment tests operator@example.com", Timeout: time.Second, Wait: func(context.Context, time.Duration) error { return nil }})
	if e != nil {
		t.Fatal(e)
	}
	facts, e := f.Facts(context.Background(), []string{"1"}, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	if e != nil || len(facts) != 2 {
		t.Fatal(facts, e)
	}
	s, _ := domain.SelectSnapshot(domain.Snapshot{Facts: facts}, time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC))
	if len(s.Facts) != 1 || s.Facts[0].Value != "100" {
		t.Fatal(s.Facts)
	}
	old, e := f.Company(context.Background(), "1")
	if e != nil || old.ID != "sec:0000000001" || old.Kind != "stock" {
		t.Fatal(old, e)
	}
}
func TestSECRejectsUnitsAndCustomConceptAmbiguity(t *testing.T) {
	facts, e := ParseCompanyFacts([]byte(`{"facts":{"us-gaap":{"NetIncomeLoss":{"units":{"EUR":[{"start":"2025-01-01","end":"2025-12-31","val":100,"accn":"a"}]}},"UnrecognizedTag":{"units":{"USD":[{"val":100,"accn":"a"}]}}},"custom":{"NetIncomeLoss":{"units":{"USD":[{"val":900,"accn":"a"}]}}}}}`), "sec:0000000001", map[string]time.Time{"a": time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	if e != nil || len(facts) != 0 {
		t.Fatal(facts, e)
	}
}
func TestSECPublicationDateUsesNextSession(t *testing.T) {
	c := domain.Calendar{CoverageStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), CoverageEnd: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC), Sessions: []domain.Session{{Date: time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), OpenAt: time.Date(2026, 3, 2, 14, 30, 0, 0, time.UTC)}}}
	v, e := PublicationTime("", "2026-03-01", c)
	if e != nil || v.Day() != 2 || v.Hour() != 14 {
		t.Fatal(v, e)
	}
}

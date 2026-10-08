package sec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSECTickerChangeKeepsIdentity(t *testing.T) {
	ticker := "OLD"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/directory" {
			fmt.Fprintf(w, `{"fields":["cik","name","ticker","exchange"],"data":[[1,"Acme",%q,"Nasdaq"]]}`, ticker)
			return
		}
		fmt.Fprintf(w, `{"cik":1,"name":"Acme","entityType":"operating","sic":"3571","tickers":[%q],"exchanges":["Nasdaq"],"filings":{"recent":{},"files":[]}}`, ticker)
	}))
	defer srv.Close()
	f, e := NewFinancials(srv.Client(), SECConfig{BaseURL: srv.URL, DirectoryURL: srv.URL + "/directory", UserAgent: "tests operator@example.com", Wait: func(context.Context, time.Duration) error { return nil }})
	if e != nil {
		t.Fatal(e)
	}
	input := []domain.Instrument{{ID: "broker-stable", Ticker: "OLD", Exchange: "NASDAQ", Kind: "unknown", Tradable: true}}
	old, e := f.Identify(context.Background(), input)
	if e != nil {
		t.Fatal(e)
	}
	ticker = "NEW"
	input[0].Ticker = ticker
	renamed, e := f.Identify(context.Background(), input)
	if e != nil || old[0].ID != renamed[0].ID || renamed[0].CIK != "0000000001" {
		t.Fatal(renamed, e)
	}
	input[0].CIK = "0000000002"
	if _, e = f.Identify(context.Background(), input); !errors.Is(e, domain.ErrVersionConflict) {
		t.Fatal(e)
	}
}
func TestSECRequestsBoundedAndIdentified(t *testing.T) {
	count := 0
	var waits []time.Duration
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing user agent")
		}
		w.Header().Set("Retry-After", "1000")
		w.WriteHeader(429)
		fmt.Fprint(w, "private upstream response")
	}))
	defer srv.Close()
	f, e := NewFinancials(srv.Client(), SECConfig{BaseURL: srv.URL, UserAgent: "tests operator@example.com", Wait: func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }})
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.Company(context.Background(), "1")
	if e == nil || count != 3 {
		t.Fatal(count, e)
	}
	for _, d := range waits {
		if d > 2*time.Second {
			t.Fatal(d)
		}
	}
	if len(waits) < 2 {
		t.Fatal(waits)
	}
}
func TestSECAccessionHistoryAndExactDecimals(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/submissions/CIK0000000001.json":
			fmt.Fprint(w, `{"cik":1,"filings":{"recent":{},"files":[{"name":"CIK0000000001-submissions-001.json"}]}}`)
		case "/submissions/CIK0000000001-submissions-001.json":
			fmt.Fprint(w, `{"accessionNumber":["old"],"filingDate":["2024-03-01"],"acceptanceDateTime":["2024-03-01T17:00:00Z"]}`)
		default:
			fmt.Fprint(w, `{"facts":{"us-gaap":{"EarningsPerShareDiluted":{"units":{"USD/shares":[{"start":"2023-01-01","end":"2023-12-31","val":1.123456789012345,"accn":"old"}]}}}}}`)
		}
	}))
	defer srv.Close()
	f, _ := NewFinancials(srv.Client(), SECConfig{BaseURL: srv.URL, UserAgent: "tests operator@example.com", Wait: func(context.Context, time.Duration) error { return nil }})
	facts, e := f.Facts(context.Background(), []string{"1"}, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	if e != nil || len(facts) != 1 || facts[0].Value != "1.123456789012345" {
		t.Fatal(facts, e)
	}
	// Identical aliases do not duplicate; disagreeing aliases are deliberately unavailable.
	data := map[string]any{"facts": map[string]any{"us-gaap": map[string]any{"Revenues": map[string]any{"units": map[string]any{"USD": []map[string]any{{"start": "2023-01-01", "end": "2023-12-31", "val": 100, "accn": "old"}}}}, "SalesRevenueNet": map[string]any{"units": map[string]any{"USD": []map[string]any{{"start": "2023-01-01", "end": "2023-12-31", "val": 200, "accn": "old"}}}}}}}
	b, _ := json.Marshal(data)
	facts, e = ParseCompanyFacts(b, "stable", map[string]time.Time{"old": time.Date(2024, 3, 1, 17, 0, 0, 0, time.UTC)}, time.Now())
	if e != nil || len(facts) != 0 {
		t.Fatal(facts, e)
	}
}

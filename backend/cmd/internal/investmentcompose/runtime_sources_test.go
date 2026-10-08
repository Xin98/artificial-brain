package investmentcompose

import (
	"context"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

type localProviderTransport struct {
	target *url.URL
	inner  http.RoundTripper
}

func (s localProviderTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = s.target.Scheme
	u.Host = s.target.Host
	copy.URL = &u
	return s.inner.RoundTrip(copy)
}
func testRealSource(t *testing.T, news string, onNews ...func()) (*Runtime, *int, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required for real composition with local providers")
	}
	pool, e := pgxpool.New(context.Background(), dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	asset := "zz-review-" + newID()
	directoryCalls := 0
	location, e := time.LoadLocation("America/New_York")
	if e != nil {
		t.Fatal(e)
	}
	local := time.Now().In(location)
	day := time.Date(local.Year(), local.Month(), local.Day()-1, 0, 0, 0, 0, location)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("provider mutation", r.Method)
			w.WriteHeader(405)
			return
		}
		switch r.URL.Path {
		case "/v2/assets":
			fmt.Fprintf(w, `[{"id":%q,"symbol":"REVIEW","name":"Review Common Stock","class":"us_equity","exchange":"NASDAQ","status":"active","tradable":true}]`, asset)
		case "/v2/assets/REVIEW":
			fmt.Fprintf(w, `{"id":%q,"symbol":"REVIEW","name":"Review Common Stock","class":"us_equity","exchange":"NASDAQ","status":"active","tradable":true}`, asset)
		case "/v2/calendar":
			fmt.Fprintf(w, `[{"date":%q,"open":"09:30","close":"16:00"},{"date":%q,"open":"09:30","close":"16:00"},{"date":%q,"open":"09:30","close":"16:00"},{"date":%q,"open":"09:30","close":"16:00"}]`, day.AddDate(0, 0, -1).Format("2006-01-02"), day.Format("2006-01-02"), day.AddDate(0, 0, 1).Format("2006-01-02"), day.AddDate(0, 0, 2).Format("2006-01-02"))
		case "/v2/stocks/bars":
			fmt.Fprintf(w, `{"bars":{"REVIEW":[{"t":%q,"o":100,"h":102,"l":99,"c":101,"v":20000}]},"next_page_token":null}`, day.UTC().Format(time.RFC3339))
		case "/v1/corporate-actions":
			fmt.Fprint(w, `{"corporate_actions":{},"next_page_token":null}`)
		case "/files/company_tickers_exchange.json":
			directoryCalls++
			fmt.Fprint(w, `{"fields":["cik","name","ticker","exchange"],"data":[[1,"Review","REVIEW","Nasdaq"]]}`)
		case "/submissions/CIK0000000001.json":
			fmt.Fprint(w, `{"cik":1,"name":"Review","entityType":"operating","sic":"3571","tickers":["REVIEW"],"exchanges":["Nasdaq"],"filings":{"recent":{},"files":[]}}`)
		case "/api/xbrl/companyfacts/CIK0000000001.json":
			fmt.Fprint(w, `{"cik":1,"facts":{}}`)
		case "/v1beta1/news":
			if len(onNews) > 0 {
				onNews[0]()
			}
			if news == "timeout" {
				<-r.Context().Done()
				return
			}
			w.Header().Set("Retry-After", "0")
			if news == "429" {
				w.WriteHeader(429)
			} else {
				w.WriteHeader(403)
			}
		case "/":
			fmt.Fprint(w, "<html>SEC homepage</html>")
		default:
			t.Error("unexpected provider path", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	original := http.DefaultTransport
	http.DefaultTransport = localProviderTransport{target, srv.Client().Transport}
	t.Cleanup(func() { http.DefaultTransport = original })
	runtime, e := New(config.InvestmentConfig{Mode: "alpaca_sec", Feed: "iex", AlpacaKey: "fixture-key", AlpacaSecret: "fixture-value", SECUserAgent: "Review operator@example.com", Timeout: 2 * time.Second, NewsEnabled: news != ""}, pool, func() time.Time { return time.Now().UTC() })
	if e != nil {
		t.Fatal(e)
	}
	return runtime, &directoryCalls, asset
}
func syncReviewSource(t *testing.T, r *Runtime) error {
	t.Helper()
	return r.Jobs.Source.Sync(context.Background(), dto.SyncRequest{Mode: r.Mode, DatasetVersion: r.DatasetVersion, InstrumentIDs: []string{"REVIEW"}, From: r.Now().AddDate(0, 0, -2), To: r.Now()})
}
func TestRealCompositionUsesSECDirectoryJSON(t *testing.T) {
	r, calls, _ := testRealSource(t, "")
	if e := syncReviewSource(t, r); e != nil || *calls != 1 {
		t.Fatal("composed SEC directory", *calls, e)
	}
}
func TestOptionalNewsFailureDoesNotAbortRealSource(t *testing.T) {
	for _, failure := range []string{"403", "429", "timeout"} {
		t.Run(failure, func(t *testing.T) {
			r, _, _ := testRealSource(t, failure)
			if e := syncReviewSource(t, r); e != nil {
				t.Fatal("news alone blocked market/financial maintenance", e)
			}
			s, e := r.Data.Read(context.Background(), r.Mode, r.Now())
			if e != nil || !strings.Contains(strings.Join(s.QualityFlags, ","), "news_unavailable") {
				t.Fatal(s.QualityFlags, e)
			}
		})
	}
}

func TestNewsStatusPersistenceFailureStillAbortsSource(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _, _ := testRealSource(t, "403", cancel)
	e := r.Jobs.Source.Sync(ctx, dto.SyncRequest{Mode: r.Mode, DatasetVersion: r.DatasetVersion, InstrumentIDs: []string{"REVIEW"}, From: r.Now().AddDate(0, 0, -2), To: r.Now()})
	if e == nil {
		t.Fatal("cancelled status transaction was hidden as optional news failure")
	}
}

func TestCompositionResolvesReusedTickerFromCurrentProviderIdentity(t *testing.T) {
	r, _, asset := testRealSource(t, "")
	at := r.Now()
	seed := domain.Snapshot{ID: "identity-seed/" + newID(), Mode: r.Mode, Feed: "iex", DatasetVersion: r.DatasetVersion, AsOf: at, Instruments: []domain.Instrument{{ID: "0-retired-" + newID(), Ticker: "REVIEW", CIK: "0000000001", Exchange: "NASDAQ", Kind: "stock", SIC: "3571", Tradable: false, Provenance: domain.Provenance{Source: "test-retired", SourceRecordID: newID(), IngestedAt: at}}}}
	if e := r.UOW.Run(context.Background(), func(ctx context.Context) error { return r.Snapshots.Insert(ctx, seed) }); e != nil {
		t.Fatal(e)
	}
	if e := syncReviewSource(t, r); e != nil {
		t.Fatal(e)
	}
	s, e := r.Data.Read(context.Background(), r.Mode, r.Now())
	if e != nil {
		t.Fatal(e)
	}
	for _, instrument := range s.Instruments {
		if instrument.ID == asset {
			if instrument.CIK != "0000000001" || instrument.Kind != "stock" {
				t.Fatal("financial metadata attached to obsolete ticker identity", instrument.ID, instrument.CIK, instrument.Kind)
			}
			return
		}
	}
	t.Fatal("current provider identity missing")
}

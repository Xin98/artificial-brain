package alpaca

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMarketAdapterReadOnlyAndPaged(t *testing.T) {
	orders := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || strings.Contains(r.URL.Path, "orders") {
			orders++
			t.Error(r.Method, r.URL.Path)
		}
		if r.Header.Get("APCA-API-KEY-ID") != "test-key" {
			t.Error("missing auth")
		}
		switch r.URL.Path {
		case "/v2/assets":
			fmt.Fprint(w, `[{"id":"asset-a","symbol":"ACME","name":"Acme Common Stock","class":"us_equity","exchange":"NASDAQ","status":"active","tradable":true}]`)
		case "/v2/calendar":
			fmt.Fprint(w, `[{"date":"2026-10-05","open":"09:30","close":"16:00"},{"date":"2026-10-06","open":"09:30","close":"16:00"}]`)
		case "/v2/stocks/bars":
			if r.URL.Query().Get("feed") != "sip" || r.URL.Query().Get("adjustment") != "raw" {
				t.Error("silently changed feed or adjustment")
			}
			if r.URL.Query().Get("page_token") == "" {
				fmt.Fprint(w, `{"bars":{"ACME":[{"t":"2026-10-05T04:00:00Z","o":100,"h":102,"l":99,"c":101,"v":20000}]},"next_page_token":"next"}`)
			} else {
				fmt.Fprint(w, `{"bars":{"ACME":[{"t":"2026-10-06T04:00:00Z","o":101,"h":103,"l":100,"c":102,"v":21000}]},"next_page_token":null}`)
			}
		case "/v1/corporate-actions":
			if r.URL.Query().Get("data_quality") != "all" {
				t.Error("incomplete actions hidden")
			}
			if r.URL.Query().Get("page_token") == "" {
				fmt.Fprint(w, `{"corporate_actions":{"forward_splits":[{"id":"split-a","symbol":"ACME","new_rate":2,"old_rate":1,"ex_date":"2026-10-06","process_date":"2026-10-05"}]},"next_page_token":"actions-next"}`)
			} else {
				fmt.Fprint(w, `{"corporate_actions":{"cash_dividends":[{"id":"dividend-a","symbol":"ACME","rate":0.5,"ex_date":"2026-10-06","process_date":"2026-10-05","payable_date":"2026-10-20"}]},"next_page_token":null}`)
			}
		default:
			t.Error("unexpected endpoint", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	m, e := NewMarket(srv.Client(), MarketConfig{DataURL: srv.URL, ReadOnlyTradingURL: srv.URL, Feed: "sip", Key: "test-key", Secret: "test-secret", Timeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	ins, e := m.Instruments(ctx, nil)
	if e != nil || len(ins) != 1 || ins[0].ID != "asset-a" {
		t.Fatal(ins, e)
	}
	from := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 20)
	bars, e := m.Bars(ctx, []string{"asset-a"}, from, to)
	if e != nil || len(bars) != 2 || bars[0].InstrumentID != "asset-a" || bars[0].Open != 100000000 {
		t.Fatal(bars, e)
	}
	actions, e := m.Actions(ctx, []string{"asset-a"}, from, to)
	if e != nil || len(actions) != 2 || actions[0].RatioNumerator != 2 || actions[1].Amount != 500000 || orders != 0 {
		t.Fatal(actions, e, orders)
	}
}
func TestMarket429TimeoutAndEntitlement(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(status)
			}))
			defer srv.Close()
			m, _ := NewMarket(srv.Client(), MarketConfig{DataURL: srv.URL, ReadOnlyTradingURL: srv.URL, Feed: "iex", Key: "test", Secret: "test", Timeout: time.Second})
			_, e := m.Instruments(context.Background(), nil)
			if e == nil {
				t.Fatal("missing failure")
			}
			want := 1
			if status == 429 {
				want = 3
			}
			if calls != want {
				t.Fatal(calls, want)
			}
		})
	}
}

func TestMarketDeadlineAndRedirectDoNotLeakCredentials(t *testing.T) {
	received := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received++ }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	m, _ := NewMarket(redirect.Client(), MarketConfig{DataURL: redirect.URL, ReadOnlyTradingURL: redirect.URL, Feed: "iex", Key: "test-key", Secret: "test-secret", Timeout: time.Second})
	if _, e := m.Instruments(context.Background(), nil); e == nil || received != 0 {
		t.Fatal("redirect followed", received, e)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	m, _ = NewMarket(slow.Client(), MarketConfig{DataURL: slow.URL, ReadOnlyTradingURL: slow.URL, Feed: "iex", Key: "test-key", Secret: "test-secret", Timeout: 20 * time.Millisecond})
	if _, e := m.Instruments(context.Background(), nil); e == nil {
		t.Fatal("deadline ignored")
	}
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/cmd/internal/investmentcompose"
	investmenthttp "github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/inbound/http"
	investmentriver "github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/river"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/platform/config"
	riverqueue "github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// The production composition accepts a clock; there is no HTTP clock or cash override.
func TestInvestmentClosedLoopRestartCompetitionAndSettlement(t *testing.T) {
	root, pool := setupAPIHandlerWithPool(t, true)
	ctx := context.Background()
	var clock atomic.Int64
	set := func(s string) {
		v, e := time.Parse(time.RFC3339, s)
		if e != nil {
			t.Fatal(e)
		}
		clock.Store(v.UnixNano())
	}
	now := func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	set("2026-10-06T21:00:00Z")
	client, e := riverqueue.NewClient(riverpgxv5.New(pool), &riverqueue.Config{})
	if e != nil {
		t.Fatal(e)
	}
	cfg := config.InvestmentConfig{Mode: "fixture"}
	mux := http.NewServeMux()
	investmenthttp.RegisterRoutes(mux, newAuthMiddleware(pool), buildInvestmentHandler(cfg, pool, client, now))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/investment/") {
			mux.ServeHTTP(w, r)
		} else {
			root.ServeHTTP(w, r)
		}
	}))
	defer srv.Close()
	owner, principal := loginViaDevInbox(t, srv, "+8613900020011")
	other, otherPrincipal := loginViaDevInbox(t, srv, "+8613900020012")
	// An unconfigured real reader never silently substitutes fixture bars.
	realMux := http.NewServeMux()
	investmenthttp.RegisterRoutes(realMux, newAuthMiddleware(pool), buildInvestmentHandler(config.InvestmentConfig{Mode: "alpaca_sec", Feed: "sip"}, pool, client, now))
	realServer := httptest.NewServer(realMux)
	defer realServer.Close()
	status, realStatus := investmentRequest(t, owner, "GET", realServer.URL+"/api/v1/investment/data-status", "", "")
	if status != 200 || realStatus["mode"] != "alpaca_sec" || realStatus["market"].(map[string]any)["state"] != "not_configured" {
		t.Fatal(status, realStatus)
	}
	status, realInstruments := investmentRequest(t, owner, "GET", realServer.URL+"/api/v1/investment/instruments", "", "")
	if status != 200 || len(realInstruments["items"].([]any)) != 0 {
		t.Fatal("real source fabricated instruments", status, realInstruments)
	}
	base := srv.URL + "/api/v1/investment"
	req := func(c *http.Client, method, path, key, body string, want int) map[string]any {
		t.Helper()
		status, v := investmentRequest(t, c, method, base+path, key, body)
		if status != want {
			t.Fatalf("%s %s: %d want %d: %v", method, path, status, want, v)
		}
		return v
	}
	a := req(owner, "POST", "/accounts", "closed-account", `{"name":"clock scenario","initialCash":"100000.00"}`, 201)
	id := a["id"].(string)
	b := req(other, "POST", "/accounts", "other-account", `{"name":"other clock scenario","initialCash":"100000.00"}`, 201)
	otherID := b["id"].(string)
	req(other, "GET", "/accounts/"+id, "", "", 404)
	req(owner, "GET", "/accounts/"+otherID, "", "", 404)
	account := func() map[string]any { return req(owner, "GET", "/accounts/"+id, "", "", 200) }
	order := func(side, key string) string {
		t.Helper()
		a = account()
		quantity := "2"
		if side == "sell" {
			quantity = "1"
		}
		body := fmt.Sprintf(`{"instrumentId":"fixture-26","side":%q,"quantity":%q,"expectedVersion":%.0f}`, side, quantity, a["version"])
		o := req(owner, "POST", "/accounts/"+id+"/orders", key, body, 201)
		retry := req(owner, "POST", "/accounts/"+id+"/orders", key, body, 201)
		if o["id"] != retry["id"] || o["state"] != "pending" {
			t.Fatal(o, retry)
		}
		return o["id"].(string)
	}
	buy := order("buy", "closed-buy")
	runtime := func() *investmentcompose.Runtime {
		t.Helper()
		r, err := investmentcompose.New(cfg, pool, now)
		if err != nil {
			t.Fatal(err)
		}
		r.SetScheduler(&investmentriver.Scheduler{Client: client})
		return r
	}
	r1, r2 := runtime(), runtime()
	args := dto.InvestmentJobArgs{JobType: "account", WorkspaceID: principal.WorkspaceID, OwnerUserID: principal.UserID, AccountID: id}
	maintain := func(r *investmentcompose.Runtime) {
		t.Helper()
		if err := r.Jobs.HandleJob(ctx, args, false); err != nil {
			t.Fatal(err)
		}
	}
	compete := func() {
		t.Helper()
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, r := range []*investmentcompose.Runtime{r1, r2} {
			wg.Add(1)
			go func(r *investmentcompose.Runtime) { defer wg.Done(); errs <- r.Jobs.HandleJob(ctx, args, false) }(r)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	fills := func(orderID string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, "select count(*) from investment.fills where account_id=$1 and order_id=$2", id, orderID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// Automation traverses the same frozen evaluation and transactionally scheduled job seam.
	toggle := fmt.Sprintf(`{"enabled":true,"expectedVersion":%.0f,"strategyVersionId":%q,"universeVersionId":%q,"policy":%s}`, b["version"], b["strategyVersionId"], b["universeVersionId"], mustInvestmentJSON(t, b["policy"]))
	req(other, "PUT", "/accounts/"+otherID+"/automation", "closed-enable", toggle, 200)
	otherArgs := dto.InvestmentJobArgs{JobType: "account", WorkspaceID: otherPrincipal.WorkspaceID, OwnerUserID: otherPrincipal.UserID, AccountID: otherID}
	if err := r1.Jobs.HandleJob(ctx, otherArgs, false); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `select args from river_job where kind='investment_job' and args->>'AccountID'=$1 and args->>'JobType'='evaluate' order by id desc limit 1`, otherID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var evaluation dto.InvestmentJobArgs
	if err := json.Unmarshal(raw, &evaluation); err != nil {
		t.Fatal(err)
	}
	if err := r2.Jobs.HandleJob(ctx, evaluation, false); err != nil {
		t.Fatal(err)
	}
	var batches int
	if err := pool.QueryRow(ctx, "select count(*) from investment.evaluation_runs where account_id=$1 and issued_orders", otherID).Scan(&batches); err != nil || batches != 1 {
		t.Fatal(batches, err)
	}
	set("2026-10-07T13:31:00Z")
	maintain(r1)
	if fills(buy) != 0 {
		t.Fatal("daily bar was not available at open")
	}
	req(owner, "POST", "/accounts/"+id+"/orders/"+buy+"/cancel", "closed-cancel", `{"expectedVersion":1}`, 409)
	if account()["cash"].(map[string]any)["reserved"] == "0.00" {
		t.Fatal("effective order reservation released")
	}
	set("2026-10-07T21:00:00Z")
	compete()
	maintain(runtime())
	if fills(buy) != 1 {
		t.Fatal("duplicate buy after competition/restart")
	}
	positions := req(owner, "GET", "/accounts/"+id+"/positions", "", "", 200)["items"].([]any)
	if len(positions) != 1 || positions[0].(map[string]any)["quantity"] != "1" {
		t.Fatal(positions)
	}
	sell := order("sell", "closed-sell")
	set("2026-10-08T21:00:00Z")
	compete()
	maintain(runtime())
	if fills(sell) != 1 {
		t.Fatal("duplicate sell after competition/restart")
	}
	cashBefore := account()["cash"].(map[string]any)
	if cashBefore["unsettled"] == "0.00" {
		t.Fatal("sale proceeds settled on trade day")
	}
	set("2026-10-09T03:59:59Z")
	maintain(r1)
	if account()["cash"].(map[string]any)["unsettled"] != cashBefore["unsettled"] {
		t.Fatal("settled before NY T+1 boundary")
	}
	set("2026-10-09T04:00:00Z")
	compete()
	maintain(runtime())
	cashAfter := account()["cash"].(map[string]any)
	if cashAfter["unsettled"] != "0.00" || cashAfter["available"] == cashBefore["available"] {
		t.Fatal(cashBefore, cashAfter)
	}
	var settlements int
	if err := pool.QueryRow(ctx, "select count(*) from investment.ledger_entries where account_id=$1 and kind='settlement'", id).Scan(&settlements); err != nil || settlements != 1 {
		t.Fatal(settlements, err)
	}
	req(other, "GET", "/accounts/"+id+"/ledger", "", "", 404)
}

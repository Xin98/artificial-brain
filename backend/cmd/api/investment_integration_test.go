package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func investmentRequest(t *testing.T, c *http.Client, method, url, key, body string) (int, map[string]any) {
	t.Helper()
	r, e := http.NewRequest(method, url, bytes.NewBufferString(body))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	resp, e := c.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(resp.Body)
	if e != nil {
		t.Fatal(e)
	}
	var v map[string]any
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(string(b), e)
	}
	return resp.StatusCode, v
}
func TestInvestmentIdempotencyAfterTimeout(t *testing.T) {
	handler, _ := setupAPIHandlerWithPool(t, true)
	srv := httptest.NewServer(handler)
	defer srv.Close()
	owner, _ := loginViaDevInbox(t, srv, "+8613900020001")
	other, _ := loginViaDevInbox(t, srv, "+8613900020002")
	base := srv.URL + "/api/v1/investment"
	body := `{"name":"我的模拟账户","initialCash":"100000.00"}`
	code, a := investmentRequest(t, owner, "POST", base+"/accounts", "create-intent", body)
	if code != 201 {
		t.Fatal(code, a)
	}
	id := a["id"].(string)
	code, retry := investmentRequest(t, owner, "POST", base+"/accounts", "create-intent", body)
	if code != 201 || retry["id"] != id {
		t.Fatal(code, retry)
	}
	code, v := investmentRequest(t, owner, "POST", base+"/accounts", "create-intent", `{"name":"新意图","initialCash":"100000.00"}`)
	if code != 409 {
		t.Fatal(code, v)
	}
	if a["automationEnabled"] != false || a["cash"].(map[string]any)["available"] != "100000.00" {
		t.Fatal(a)
	}
	for _, suffix := range []string{"", "/positions", "/orders", "/ledger", "/evaluations", "/automation-events", "/performance"} {
		code, v = investmentRequest(t, other, "GET", base+"/accounts/"+id+suffix, "", "")
		if code != 404 {
			t.Fatal(suffix, code, v)
		}
	}
	code, v = investmentRequest(t, other, "POST", base+"/accounts/"+id+"/orders", "other-order", `{"instrumentId":"fixture-26","side":"buy","quantity":"1","expectedVersion":1}`)
	if code != 404 {
		t.Fatal(code, v)
	}
	for _, bad := range []string{`{"initialCash":100000}`, `{"initialCash":"1.001"}`, `{"ownerUserId":"fake"}`, `{"initialCash":"1.00","unknown":true}`} {
		code, v = investmentRequest(t, owner, "POST", base+"/accounts", "bad-"+bad, bad)
		if code != 422 {
			t.Fatal(code, v)
		}
	}
	// A lost-response retry must return the same committed order without reserving twice.
	code, a = investmentRequest(t, owner, "GET", base+"/accounts/"+id, "", "")
	if code != 200 {
		t.Fatal(code, a)
	}
	orderBody := fmt.Sprintf(`{"instrumentId":"fixture-26","side":"buy","quantity":"1","expectedVersion":%.0f}`, a["version"])
	code, o := investmentRequest(t, owner, "POST", base+"/accounts/"+id+"/orders", "order-intent", orderBody)
	if code != 201 {
		t.Fatal(code, o)
	}
	code, v = investmentRequest(t, owner, "POST", base+"/accounts/"+id+"/orders", "order-intent", orderBody)
	if code != 201 || v["id"] != o["id"] {
		t.Fatal(code, v)
	}
	code, a = investmentRequest(t, owner, "GET", base+"/accounts/"+id, "", "")
	if code != 200 {
		t.Fatal(code, a)
	}
	toggle := fmt.Sprintf(`{"enabled":true,"expectedVersion":%.0f,"strategyVersionId":%q,"universeVersionId":%q,"policy":%s}`, a["version"], a["strategyVersionId"], a["universeVersionId"], mustInvestmentJSON(t, a["policy"]))
	code, v = investmentRequest(t, owner, "PUT", base+"/accounts/"+id+"/automation", "auto-intent", toggle)
	if code != 200 {
		t.Fatal(code, v)
	}
	code, retry = investmentRequest(t, owner, "PUT", base+"/accounts/"+id+"/automation", "auto-intent", toggle)
	if code != 200 || v["version"] != retry["version"] {
		t.Fatal(code, retry)
	}
	backtest := fmt.Sprintf(`{"strategyVersionId":%q,"universeVersionId":%q,"from":"2026-07-01T00:00:00Z","to":"2026-07-31T00:00:00Z"}`, a["strategyVersionId"], a["universeVersionId"])
	code, v = investmentRequest(t, owner, "POST", base+"/backtests", "backtest-intent", backtest)
	if code != 202 {
		t.Fatal(code, v)
	}
	code, retry = investmentRequest(t, owner, "POST", base+"/backtests", "backtest-intent", backtest)
	if code != 202 || v["runId"] != retry["runId"] {
		t.Fatal(code, retry)
	}
	code, v = investmentRequest(t, other, "GET", base+"/backtests/"+v["runId"].(string), "", "")
	if code != 404 {
		t.Fatal(code, v)
	}
}
func mustInvestmentJSON(t *testing.T, v any) string {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestInvestmentDatabaseFailurePreservesSession(t *testing.T) {
	handler, pool := setupAPIHandlerWithPool(t, true)
	srv := httptest.NewServer(handler)
	defer srv.Close()
	owner, _ := loginViaDevInbox(t, srv, "+8613900020003")
	pool.Close()
	code, v := investmentRequest(t, owner, "GET", srv.URL+"/api/v1/investment/accounts", "", "")
	if code != 503 {
		t.Fatal(code, v)
	}
}

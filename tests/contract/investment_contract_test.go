package contract

import (
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"testing"
)

func TestInvestmentContractRoutesAndDecimalSchemas(t *testing.T) {
	b, e := os.ReadFile(filepath.Join("..", "..", "contracts", "openapi", "investment.yaml"))
	if e != nil {
		t.Fatal(e)
	}
	var doc map[string]any
	if e = yaml.Unmarshal(b, &doc); e != nil {
		t.Fatal(e)
	}
	if doc["openapi"] != "3.1.1" {
		t.Fatal(doc["openapi"])
	}
	paths := doc["paths"].(map[string]any)
	routes := map[string][]string{
		"data-status": {"get"}, "data-sync": {"post"}, "data-sync/{id}": {"get"}, "data-syncs": {"get"},
		"instruments": {"get"}, "instruments/{id}/analysis": {"get"}, "universes": {"get", "post"}, "universes/{id}/versions": {"post"},
		"strategies": {"get"}, "strategies/{id}/versions": {"post"}, "accounts": {"get", "post"}, "accounts/{id}": {"get"},
		"accounts/{id}/positions": {"get"}, "accounts/{id}/ledger": {"get"}, "accounts/{id}/orders": {"get", "post"},
		"accounts/{id}/orders/{orderId}/cancel": {"post"}, "accounts/{id}/automation": {"put"}, "accounts/{id}/evaluations": {"get", "post"},
		"accounts/{id}/evaluations/{runId}": {"get"}, "accounts/{id}/automation-events": {"get"}, "accounts/{id}/performance": {"get"},
		"backtests": {"get", "post"}, "backtests/{id}": {"get"},
	}
	for path, methods := range routes {
		item, ok := paths["/api/v1/investment/"+path].(map[string]any)
		if !ok {
			t.Fatal(path)
		}
		for _, method := range methods {
			op, ok := item[method].(map[string]any)
			if !ok {
				t.Fatal(path, method)
			}
			rs := op["responses"].(map[string]any)
			for _, code := range []string{"401", "404", "422", "503"} {
				if rs[code] == nil {
					t.Fatal(path, method, code)
				}
			}
			if method != "get" {
				if rs["409"] == nil || op["requestBody"] == nil {
					t.Fatal(path, method)
				}
				params := op["parameters"].([]any)
				found := false
				for _, p := range params {
					m := p.(map[string]any)
					if m["name"] == "Idempotency-Key" && m["required"] == true {
						found = true
					}
				}
				if !found {
					t.Fatal(path, "no idempotency")
				}
			}
		}
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for name, raw := range schemas {
		s := raw.(map[string]any)
		if s["type"] == "object" && s["additionalProperties"] != false {
			t.Fatal(name, "open object")
		}
	}
	for _, name := range []string{"Money", "SignedMoney", "Price", "Quantity"} {
		s := schemas[name].(map[string]any)
		if s["type"] != "string" || s["pattern"] == nil {
			t.Fatal(name, s)
		}
	}
	metric := schemas["Metric"].(map[string]any)["properties"].(map[string]any)["value"].(map[string]any)
	if metric["anyOf"] == nil {
		t.Fatal(metric)
	}
	for _, name := range []string{"NAVPoint", "Position"} {
		p := schemas[name].(map[string]any)["properties"].(map[string]any)
		field := "nav"
		if name == "Position" {
			field = "costBasis"
		}
		if p[field].(map[string]any)["$ref"] != "#/components/schemas/Money" {
			t.Fatal(name, field, p[field])
		}
	}
	delta := schemas["Balances"].(map[string]any)["properties"].(map[string]any)
	if delta["available"].(map[string]any)["$ref"] != "#/components/schemas/SignedMoney" {
		t.Fatal("ledger signed delta", delta)
	}
}

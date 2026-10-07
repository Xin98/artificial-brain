package dto

import (
	"encoding/json"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"strings"
	"testing"
)

func TestInvestmentPublicDecimals(t *testing.T) {
	v := struct {
		Point domain.NAVPoint
		Fill  domain.Fill
		Delta domain.Balances
		Scope domain.Scope
	}{Point: domain.NAVPoint{NAV: 12345}, Fill: domain.Fill{Price: 1234567, Quantity: 10, Gross: 1234, Fee: 1}, Delta: domain.Balances{Available: -100}, Scope: domain.Scope{OwnerUserID: "private"}}
	b, e := json.Marshal(Public(v))
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{`"nav":"123.45"`, `"price":"1.234567"`, `"quantity":"10"`, `"available":"-1.00"`} {
		if !strings.Contains(string(b), s) {
			t.Fatal(string(b))
		}
	}
	if strings.Contains(string(b), "private") || strings.Contains(string(b), "scope") {
		t.Fatal("scope leaked", string(b))
	}
}

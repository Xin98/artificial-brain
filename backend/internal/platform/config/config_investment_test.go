package config

import (
	"testing"
)

func TestInvestmentConfigFixtureDoesNotNeedSecrets(t *testing.T) {
	readSecrets := 0
	lookup := mapLookup(map[string]string{"DATABASE_URL": "postgres://test:test@localhost/test", "APP_ENV": "development", "REMINDER_RECEIPT_SECRET": "fixture-secret"})
	cfg, e := Load(RoleAPI, func(key string) (string, bool) {
		if key == "INVESTMENT_ALPACA_KEY" || key == "INVESTMENT_ALPACA_SECRET" {
			readSecrets++
		}
		return lookup(key)
	})
	if e != nil || cfg.Investment.Mode != "fixture" || readSecrets != 0 {
		t.Fatal(cfg.Investment, e, readSecrets)
	}
}

package config

import (
	"testing"
)

func TestInvestmentNewsAndSECOptions(t *testing.T) {
	lookup := func(key string) (string, bool) {
		if key == "INVESTMENT_NEWS_ENABLED" {
			return "yes", true
		}
		return "", false
	}
	if _, e := loadInvestment(lookup); e == nil {
		t.Fatal("ambiguous news option")
	}
	lookup = func(key string) (string, bool) {
		switch key {
		case "INVESTMENT_DATA_MODE":
			return "alpaca_sec", true
		case "INVESTMENT_SEC_USER_AGENT":
			return "tests operator@example.com", true
		case "INVESTMENT_NEWS_ENABLED":
			return "true", true
		}
		return "", false
	}
	c, e := loadInvestment(lookup)
	if e != nil || !c.NewsEnabled || c.SECUserAgent == "" {
		t.Fatal(c, e)
	}
}

package config

import (
	"fmt"
	"time"
)

type InvestmentConfig struct {
	Mode, Feed, AlpacaKey, AlpacaSecret, SettlementCalendarFile, SECUserAgent string
	Timeout                                                                   time.Duration
	NewsEnabled                                                               bool
}

func loadInvestment(lookup LookupEnv) (InvestmentConfig, error) {
	c := InvestmentConfig{Mode: valueOrDefault(lookup, "INVESTMENT_DATA_MODE", "fixture"), Feed: valueOrDefault(lookup, "INVESTMENT_ALPACA_FEED", "iex")}
	if c.Mode != "fixture" && c.Mode != "alpaca_sec" {
		return c, fmt.Errorf("config: invalid INVESTMENT_DATA_MODE")
	}
	if c.Feed != "iex" && c.Feed != "sip" {
		return c, fmt.Errorf("config: invalid INVESTMENT_ALPACA_FEED")
	}
	var e error
	c.Timeout, e = duration(lookup, "INVESTMENT_DATA_TIMEOUT", 15*time.Second)
	if e != nil || c.Timeout <= 0 || c.Timeout > 60*time.Second {
		return c, fmt.Errorf("config: invalid INVESTMENT_DATA_TIMEOUT")
	}
	c.SettlementCalendarFile = valueOrDefault(lookup, "INVESTMENT_SETTLEMENT_CALENDAR_FILE", "")
	news := valueOrDefault(lookup, "INVESTMENT_NEWS_ENABLED", "false")
	if news != "true" && news != "false" {
		return c, fmt.Errorf("config: invalid INVESTMENT_NEWS_ENABLED")
	}
	c.NewsEnabled = news == "true"
	if c.Mode == "alpaca_sec" {
		c.AlpacaKey = valueOrDefault(lookup, "INVESTMENT_ALPACA_KEY", "")
		c.AlpacaSecret = valueOrDefault(lookup, "INVESTMENT_ALPACA_SECRET", "")
		c.SECUserAgent = valueOrDefault(lookup, "INVESTMENT_SEC_USER_AGENT", "")
	}
	return c, nil
}

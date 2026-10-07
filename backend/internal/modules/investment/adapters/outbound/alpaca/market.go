package alpaca

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type MarketConfig struct {
	DataURL, ReadOnlyTradingURL, Feed, Key, Secret string
	Timeout                                        time.Duration
	SettlementCalendar                             *domain.Calendar
}
type Market struct {
	cfg         MarketConfig
	http        *readClient
	mu          sync.RWMutex
	instruments map[string]domain.Instrument
}

func NewMarket(client *http.Client, cfg MarketConfig) (*Market, error) {
	if !validBase(cfg.DataURL) || !validBase(cfg.ReadOnlyTradingURL) || (cfg.Feed != "iex" && cfg.Feed != "sip") || cfg.Key == "" || cfg.Secret == "" || cfg.Timeout <= 0 {
		return nil, domain.ErrDataNotConfigured
	}
	return &Market{cfg: cfg, http: newReadClient(client, cfg.Key, cfg.Secret, cfg.Timeout), instruments: map[string]domain.Instrument{}}, nil
}

type asset struct {
	ID, Symbol, Name, Class, Exchange, Status string
	Tradable                                  bool
}

func (m *Market) Instruments(ctx context.Context, ids []string) ([]domain.Instrument, error) {
	var assets []asset
	if len(ids) == 0 {
		e := m.http.get(ctx, m.cfg.ReadOnlyTradingURL, "/v2/assets", url.Values{"status": {"active"}, "asset_class": {"us_equity"}}, &assets)
		if e != nil {
			return nil, e
		}
	} else {
		for _, id := range ids {
			var a asset
			if e := m.http.get(ctx, m.cfg.ReadOnlyTradingURL, "/v2/assets/"+url.PathEscape(id), url.Values{}, &a); e != nil {
				return nil, e
			}
			assets = append(assets, a)
		}
	}
	var out []domain.Instrument
	now := time.Now().UTC()
	for _, a := range assets {
		if a.ID == "" || a.Symbol == "" || a.Class != "us_equity" {
			continue
		}
		kind := "unknown"
		if a.Symbol == "SPY" {
			kind = "etf"
		}
		i := domain.Instrument{ID: a.ID, Ticker: a.Symbol, Name: a.Name, Exchange: a.Exchange, Kind: kind, Tradable: a.Tradable && a.Status == "active", Provenance: domain.Provenance{Source: "alpaca", SourceRecordID: a.ID, IngestedAt: now}}
		out = append(out, i)
		m.mu.Lock()
		m.instruments[i.ID] = i
		m.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (m *Market) resolve(ctx context.Context, ids []string) (map[string]string, []string, error) {
	symbols := make([]string, 0, len(ids))
	mapping := map[string]string{}
	for _, id := range ids {
		m.mu.RLock()
		i, ok := m.instruments[id]
		m.mu.RUnlock()
		if !ok {
			a, e := m.Instruments(ctx, []string{id})
			if e != nil {
				return nil, nil, e
			}
			if len(a) != 1 || a[0].ID != id {
				return nil, nil, domain.ErrNotFound
			}
			i = a[0]
		}
		mapping[i.Ticker] = i.ID
		symbols = append(symbols, i.Ticker)
	}
	sort.Strings(symbols)
	return mapping, symbols, nil
}
func (m *Market) Calendar(ctx context.Context, from, to time.Time) (domain.Calendar, error) {
	var rows []struct{ Date, Open, Close string }
	if e := m.http.get(ctx, m.cfg.ReadOnlyTradingURL, "/v2/calendar", url.Values{"start": {from.Format("2006-01-02")}, "end": {to.Format("2006-01-02")}}, &rows); e != nil {
		return domain.Calendar{}, e
	}
	c := domain.Calendar{Version: "alpaca-calendar/" + from.Format("2006-01-02") + "/" + to.Format("2006-01-02"), Source: "alpaca", CoverageStart: dayUTC(from), CoverageEnd: dayUTC(to).Add(24*time.Hour - time.Nanosecond), Sessions: []domain.Session{}, SettlementDays: []time.Time{}}
	loc, e := time.LoadLocation("America/New_York")
	if e != nil {
		return c, e
	}
	for _, r := range rows {
		d, e := time.Parse("2006-01-02", r.Date)
		if e != nil {
			return c, &ProviderError{Code: "provider_calendar_invalid"}
		}
		open, e := time.ParseInLocation("2006-01-02 15:04", r.Date+" "+r.Open, loc)
		if e != nil {
			return c, e
		}
		close, e := time.ParseInLocation("2006-01-02 15:04", r.Date+" "+r.Close, loc)
		if e != nil || !close.After(open) {
			return c, &ProviderError{Code: "provider_calendar_invalid"}
		}
		c.Sessions = append(c.Sessions, domain.Session{Date: d, OpenAt: open.UTC(), CloseAt: close.UTC()})
	}
	sort.Slice(c.Sessions, func(i, j int) bool { return c.Sessions[i].Date.Before(c.Sessions[j].Date) })
	if m.cfg.SettlementCalendar != nil {
		sc := m.cfg.SettlementCalendar
		c.Version += "/" + sc.Version
		c.Source += "/" + sc.Source
		if c.CoverageStart.Before(sc.CoverageStart) {
			c.CoverageStart = sc.CoverageStart
		}
		if c.CoverageEnd.After(sc.CoverageEnd) {
			c.CoverageEnd = sc.CoverageEnd
		}
		for _, d := range sc.SettlementDays {
			if !d.Before(c.CoverageStart) && !d.After(c.CoverageEnd) {
				c.SettlementDays = append(c.SettlementDays, d)
			}
		}
	}
	return c, nil
}
func dayUTC(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
func sourcePrice(n json.Number) (domain.Price, error) {
	r, ok := new(big.Rat).SetString(n.String())
	if !ok || r.Sign() <= 0 {
		return 0, domain.ErrInvalidInput
	}
	r.Mul(r, big.NewRat(1000000, 1))
	if !r.IsInt() || !r.Num().IsInt64() {
		return 0, domain.ErrOverflow
	}
	return domain.Price(r.Num().Int64()), nil
}
func (m *Market) Bars(ctx context.Context, ids []string, from, to time.Time) ([]domain.Bar, error) {
	mapping, symbols, e := m.resolve(ctx, ids)
	if e != nil {
		return nil, e
	}
	if len(symbols) == 0 {
		return []domain.Bar{}, nil
	}
	calendar, e := m.Calendar(ctx, from, to)
	if e != nil {
		return nil, e
	}
	loc, e := time.LoadLocation("America/New_York")
	if e != nil {
		return nil, e
	}
	var out []domain.Bar
	token := ""
	seen := map[string]bool{}
	for page := 0; page < 1000; page++ {
		q := url.Values{"symbols": {strings.Join(symbols, ",")}, "timeframe": {"1Day"}, "adjustment": {"raw"}, "feed": {m.cfg.Feed}, "start": {from.UTC().Format(time.RFC3339)}, "end": {to.UTC().Format(time.RFC3339)}, "limit": {"10000"}}
		if token != "" {
			q.Set("page_token", token)
		}
		var response struct {
			Bars map[string][]struct {
				T          time.Time `json:"t"`
				O, H, L, C json.Number
				V          json.Number
			}
			Next string `json:"next_page_token"`
		}
		if e = m.http.get(ctx, m.cfg.DataURL, "/v2/stocks/bars", q, &response); e != nil {
			return nil, e
		}
		symbols := make([]string, 0, len(response.Bars))
		for symbol := range response.Bars {
			symbols = append(symbols, symbol)
		}
		sort.Strings(symbols)
		for _, symbol := range symbols {
			id, ok := mapping[symbol]
			if !ok {
				return nil, &ProviderError{Code: "provider_identity_mismatch"}
			}
			for _, raw := range response.Bars[symbol] {
				date := dayUTC(raw.T.In(loc))
				_, e := calendar.Session(date)
				if e != nil {
					return nil, e
				}
				// Daily volume includes extended-hours trades. Treat the final daily
				// aggregate as known only at the following midnight in New York.
				available := time.Date(date.Year(), date.Month(), date.Day()+1, 0, 0, 0, 0, loc).UTC()
				b := domain.Bar{InstrumentID: id, SessionDate: date, AvailableAt: available, Provenance: domain.Provenance{Source: "alpaca/" + m.cfg.Feed, SourceRecordID: id + "/" + date.Format("2006-01-02"), IngestedAt: time.Now().UTC()}}
				for i, p := range []*domain.Price{&b.Open, &b.High, &b.Low, &b.Close} {
					v, e := sourcePrice([]json.Number{raw.O, raw.H, raw.L, raw.C}[i])
					if e != nil {
						return nil, e
					}
					*p = v
				}
				volume, e := strconv.ParseInt(raw.V.String(), 10, 64)
				if e != nil || volume < 0 {
					return nil, domain.ErrInvalidInput
				}
				b.Volume = domain.Quantity(volume)
				b.NoOpeningTrade = volume == 0
				out = append(out, b)
			}
		}
		if response.Next == "" {
			return out, nil
		}
		if seen[response.Next] {
			return nil, &ProviderError{Code: "provider_pagination_loop"}
		}
		seen[response.Next] = true
		token = response.Next
	}
	return nil, &ProviderError{Code: "provider_page_limit"}
}
func (m *Market) Actions(ctx context.Context, ids []string, from, to time.Time) ([]domain.CorporateAction, error) {
	mapping, symbols, e := m.resolve(ctx, ids)
	if e != nil {
		return nil, e
	}
	if len(symbols) == 0 {
		return []domain.CorporateAction{}, nil
	}
	calendar, e := m.Calendar(ctx, from, to.AddDate(0, 0, 60))
	if e != nil {
		return nil, e
	}
	var out []domain.CorporateAction
	token := ""
	seen := map[string]bool{}
	for page := 0; page < 1000; page++ {
		q := url.Values{"symbols": {strings.Join(symbols, ",")}, "start": {from.Format("2006-01-02")}, "end": {to.Format("2006-01-02")}, "data_quality": {"all"}, "limit": {"1000"}}
		if token != "" {
			q.Set("page_token", token)
		}
		var response struct {
			Actions map[string][]json.RawMessage `json:"corporate_actions"`
			Next    string                       `json:"next_page_token"`
		}
		if e = m.http.get(ctx, m.cfg.DataURL, "/v1/corporate-actions", q, &response); e != nil {
			return nil, e
		}
		kinds := make([]string, 0, len(response.Actions))
		for k := range response.Actions {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		for _, kind := range kinds {
			for _, raw := range response.Actions[kind] {
				var r struct {
					ID, Symbol    string
					ExDate        string      `json:"ex_date"`
					EffectiveDate string      `json:"effective_date"`
					PayDate       string      `json:"payable_date"`
					ProcessDate   string      `json:"process_date"`
					NewRate       json.Number `json:"new_rate"`
					OldRate       json.Number `json:"old_rate"`
					Rate          json.Number `json:"rate"`
				}
				if e = json.Unmarshal(raw, &r); e != nil {
					return nil, e
				}
				id, ok := mapping[r.Symbol]
				if !ok {
					return nil, &ProviderError{Code: "provider_identity_mismatch"}
				}
				a := domain.CorporateAction{ID: r.ID, InstrumentID: id, Kind: "unsupported", Currency: "USD", AvailableAt: time.Now().UTC(), Provenance: domain.Provenance{Source: "alpaca", SourceRecordID: r.ID, IngestedAt: time.Now().UTC()}}
				date := r.ExDate
				if date == "" {
					date = r.EffectiveDate
				}
				d, de := time.Parse("2006-01-02", date)
				if de == nil {
					session, se := calendar.Session(d)
					if se == nil {
						a.EffectiveAt = session.OpenAt
					}
				}
				switch kind {
				case "forward_splits", "reverse_splits":
					n, no := new(big.Rat).SetString(r.NewRate.String())
					o, oo := new(big.Rat).SetString(r.OldRate.String())
					if no && oo && n.Sign() > 0 && o.Sign() > 0 {
						ratio := new(big.Rat).Quo(n, o)
						if ratio.Num().IsInt64() && ratio.Denom().IsInt64() {
							a.Kind = "split"
							a.RatioNumerator = ratio.Num().Int64()
							a.RatioDenominator = ratio.Denom().Int64()
						}
					}
				case "cash_dividends":
					a.Amount, e = sourcePrice(r.Rate)
					if e == nil {
						a.Kind = "dividend"
					}
					pay, pe := time.Parse("2006-01-02", r.PayDate)
					if pe == nil {
						a.PayAt = pay
					}
				}
				if a.ID == "" {
					return nil, fmt.Errorf("provider_action_identity_missing")
				}
				if a.EffectiveAt.IsZero() || (a.Kind == "dividend" && a.PayAt.IsZero()) {
					a.Kind = "incomplete"
				}
				out = append(out, a)
			}
		}
		if response.Next == "" {
			return out, nil
		}
		if seen[response.Next] {
			return nil, &ProviderError{Code: "provider_pagination_loop"}
		}
		seen[response.Next] = true
		token = response.Next
	}
	return nil, &ProviderError{Code: "provider_page_limit"}
}

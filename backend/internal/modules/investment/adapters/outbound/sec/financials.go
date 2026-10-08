package sec

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SECConfig struct {
	BaseURL, DirectoryURL, UserAgent string
	Timeout                          time.Duration
	Calendar                         domain.Calendar
	Wait                             func(context.Context, time.Duration) error
}
type Financials struct {
	client      *http.Client
	cfg         SECConfig
	mu          sync.Mutex
	last        time.Time
	submissions map[string]submission
}
type filings struct{ AccessionNumber, FilingDate, AcceptanceDateTime []string }
type submission struct {
	CIK                   json.RawMessage `json:"cik"`
	Name, EntityType, SIC string
	Tickers, Exchanges    []string
	Filings               struct {
		Recent filings
		Files  []struct{ Name string }
	}
}
type ProviderError struct{ Code string }

func (e *ProviderError) Error() string { return "SEC: " + e.Code }
func NewFinancials(client *http.Client, cfg SECConfig) (*Financials, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://data.sec.gov"
	}
	if cfg.DirectoryURL == "" {
		cfg.DirectoryURL = "https://www.sec.gov/files/company_tickers_exchange.json"
	}
	for _, raw := range []string{cfg.BaseURL, cfg.DirectoryURL} {
		u, e := url.Parse(raw)
		if e != nil || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && (u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost"))) {
			return nil, domain.ErrInvalidInput
		}
	}
	if !strings.Contains(cfg.UserAgent, "@") || strings.ContainsAny(cfg.UserAgent, "\r\n") {
		return nil, domain.ErrDataNotConfigured
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	if client == nil {
		client = &http.Client{}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if cfg.Wait == nil {
		cfg.Wait = func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		}
	}
	return &Financials{client: &copyClient, cfg: cfg, submissions: map[string]submission{}}, nil
}
func normalizeCIK(v string) (string, error) {
	if !regexp.MustCompile(`^[0-9]{1,10}$`).MatchString(v) {
		return "", domain.ErrInvalidInput
	}
	n, e := strconv.ParseUint(v, 10, 64)
	if e != nil || n == 0 {
		return "", domain.ErrInvalidInput
	}
	return fmt.Sprintf("%010d", n), nil
}
func (f *Financials) get(ctx context.Context, endpoint string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, f.cfg.Timeout)
	defer cancel()
	for attempt := 0; attempt < 3; attempt++ {
		f.mu.Lock()
		delay := time.Until(f.last.Add(500 * time.Millisecond))
		if delay > 0 {
			if e := f.cfg.Wait(ctx, delay); e != nil {
				f.mu.Unlock()
				return nil, e
			}
		}
		f.last = time.Now()
		f.mu.Unlock()
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if e != nil {
			return nil, domain.ErrInvalidInput
		}
		req.Header.Set("User-Agent", f.cfg.UserAgent)
		req.Header.Set("Accept", "application/json")
		resp, e := f.client.Do(req)
		if e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt == 2 {
				return nil, &ProviderError{"unavailable"}
			}
			continue
		}
		b, e := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024+1))
		resp.Body.Close()
		if e != nil {
			return nil, &ProviderError{"unavailable"}
		}
		if resp.StatusCode == 200 {
			if len(b) > 32*1024*1024 {
				return nil, &ProviderError{"response_too_large"}
			}
			return b, nil
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			if attempt == 2 {
				return nil, &ProviderError{"unavailable"}
			}
			delay := time.Second
			if s, er := strconv.Atoi(resp.Header.Get("Retry-After")); er == nil && s >= 0 && s <= 2 {
				delay = time.Duration(s) * time.Second
			}
			if e = f.cfg.Wait(ctx, delay); e != nil {
				return nil, e
			}
			continue
		}
		return nil, &ProviderError{fmt.Sprintf("http_%d", resp.StatusCode)}
	}
	return nil, &ProviderError{"unavailable"}
}
func (f *Financials) load(ctx context.Context, cik string) (submission, error) {
	var s submission
	b, e := f.get(ctx, strings.TrimRight(f.cfg.BaseURL, "/")+"/submissions/CIK"+cik+".json")
	if e != nil {
		return s, e
	}
	e = json.Unmarshal(b, &s)
	if e != nil {
		return s, &ProviderError{"invalid_response"}
	}
	raw := strings.Trim(string(s.CIK), "\"")
	actual, e := normalizeCIK(raw)
	if e != nil || actual != cik {
		return s, domain.ErrVersionConflict
	}
	f.mu.Lock()
	f.submissions[cik] = s
	f.mu.Unlock()
	return s, nil
}
func (f *Financials) Company(ctx context.Context, cik string) (domain.Instrument, error) {
	cik, e := normalizeCIK(cik)
	if e != nil {
		return domain.Instrument{}, e
	}
	s, e := f.load(ctx, cik)
	if e != nil {
		return domain.Instrument{}, e
	}
	i := domain.Instrument{ID: "sec:" + cik, Name: s.Name, CIK: cik, SIC: s.SIC, Kind: "unknown", Provenance: domain.Provenance{Source: "sec-submissions", SourceRecordID: cik, IngestedAt: time.Now().UTC()}}
	if len(s.Tickers) == 1 && len(s.Exchanges) == 1 {
		i.Ticker = s.Tickers[0]
		i.Exchange = s.Exchanges[0]
	}
	if s.EntityType == "operating" && s.SIC != "" {
		i.Kind = "stock"
	}
	return i, nil
}

// PublicationTime refuses to invent a time when only a filing date is known.
func PublicationTime(accepted, filed string, calendar domain.Calendar) (time.Time, error) {
	if accepted != "" {
		v, e := time.Parse(time.RFC3339, accepted)
		if e == nil {
			return v, nil
		}
		return time.Time{}, domain.ErrInvalidInput
	}
	d, e := time.Parse("2006-01-02", filed)
	if e != nil {
		return time.Time{}, domain.ErrInvalidInput
	}
	next, e := calendar.NextSession(d.Add(24*time.Hour - time.Nanosecond))
	return next.OpenAt, e
}
func addPublications(out map[string]time.Time, fs filings, calendar domain.Calendar) error {
	if len(fs.AccessionNumber) != len(fs.FilingDate) {
		return domain.ErrInvalidInput
	}
	for n, id := range fs.AccessionNumber {
		accepted := ""
		if n < len(fs.AcceptanceDateTime) {
			accepted = fs.AcceptanceDateTime[n]
		}
		v, e := PublicationTime(accepted, fs.FilingDate[n], calendar)
		if e != nil {
			continue
		}
		if prior, ok := out[id]; ok && !prior.Equal(v) {
			return domain.ErrVersionConflict
		}
		out[id] = v
	}
	return nil
}
func (f *Financials) Facts(ctx context.Context, ciks []string, asOf time.Time) ([]domain.FinancialFact, error) {
	if len(ciks) > 100 || asOf.IsZero() {
		return nil, domain.ErrInvalidInput
	}
	var out []domain.FinancialFact
	for _, raw := range ciks {
		cik, e := normalizeCIK(raw)
		if e != nil {
			return nil, e
		}
		s, e := f.load(ctx, cik)
		if e != nil {
			return nil, e
		}
		pub := map[string]time.Time{}
		if e = addPublications(pub, s.Filings.Recent, f.cfg.Calendar); e != nil {
			return nil, e
		}
		for _, file := range s.Filings.Files {
			if !regexp.MustCompile(`^CIK[0-9]{10}-submissions-[0-9]{3}\.json$`).MatchString(file.Name) {
				return nil, domain.ErrInvalidInput
			}
			b, e := f.get(ctx, strings.TrimRight(f.cfg.BaseURL, "/")+"/submissions/"+file.Name)
			if e != nil {
				return nil, e
			}
			var fs filings
			if e = json.Unmarshal(b, &fs); e != nil {
				return nil, &ProviderError{"invalid_response"}
			}
			if e = addPublications(pub, fs, f.cfg.Calendar); e != nil {
				return nil, e
			}
		}
		b, e := f.get(ctx, strings.TrimRight(f.cfg.BaseURL, "/")+"/api/xbrl/companyfacts/CIK"+cik+".json")
		if e != nil {
			return nil, e
		}
		facts, e := ParseCompanyFacts(b, "sec:"+cik, pub, asOf)
		if e != nil {
			return nil, e
		}
		out = append(out, facts...)
	}
	return out, nil
}
func exchange(s string) string {
	switch strings.ToUpper(s) {
	case "NASDAQ":
		return "NASDAQ"
	case "NYSE":
		return "NYSE"
	case "AMEX", "NYSEMKT", "NYSE AMERICAN":
		return "AMEX"
	}
	return ""
}
func (f *Financials) Identify(ctx context.Context, instruments []domain.Instrument) ([]domain.Instrument, error) {
	b, e := f.get(ctx, f.cfg.DirectoryURL)
	if e != nil {
		return nil, e
	}
	var directory struct {
		Fields []string
		Data   [][]json.RawMessage
	}
	if e = json.Unmarshal(b, &directory); e != nil {
		return nil, &ProviderError{"invalid_response"}
	}
	indexes := map[string]int{}
	for n, name := range directory.Fields {
		indexes[name] = n
	}
	for _, field := range []string{"cik", "ticker", "exchange"} {
		if _, ok := indexes[field]; !ok {
			return nil, &ProviderError{"invalid_response"}
		}
	}
	out := append([]domain.Instrument(nil), instruments...)
	for n, i := range out {
		if i.Kind == "etf" {
			continue
		}
		match := ""
		for _, row := range directory.Data {
			if len(row) != len(directory.Fields) {
				return nil, &ProviderError{"invalid_response"}
			}
			var ticker, ex string
			json.Unmarshal(row[indexes["ticker"]], &ticker)
			json.Unmarshal(row[indexes["exchange"]], &ex)
			if ticker != i.Ticker || exchange(ex) == "" || exchange(ex) != exchange(i.Exchange) {
				continue
			}
			cik, e := normalizeCIK(strings.Trim(string(row[indexes["cik"]]), "\""))
			if e != nil {
				return nil, e
			}
			if match != "" && match != cik {
				return nil, domain.ErrVersionConflict
			}
			match = cik
		}
		if match == "" {
			continue
		}
		if i.CIK != "" {
			old, e := normalizeCIK(i.CIK)
			if e != nil || old != match {
				return nil, domain.ErrVersionConflict
			}
		}
		company, e := f.Company(ctx, match)
		if e != nil {
			return nil, e
		}
		f.mu.Lock()
		s := f.submissions[match]
		f.mu.Unlock()
		verified := false
		for k, t := range s.Tickers {
			if t == i.Ticker && k < len(s.Exchanges) && exchange(s.Exchanges[k]) == exchange(i.Exchange) {
				verified = true
			}
		}
		if !verified {
			return nil, domain.ErrVersionConflict
		}
		i.CIK = match
		i.SIC = company.SIC
		i.Kind = company.Kind
		i.Name = company.Name
		out[n] = i
	}
	return out, nil
}

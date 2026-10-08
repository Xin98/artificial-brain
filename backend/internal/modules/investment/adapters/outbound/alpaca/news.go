package alpaca

import (
	"context"
	"encoding/json"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type NewsConfig struct {
	Enabled              bool
	BaseURL, Key, Secret string
	Timeout              time.Duration
	Instruments          []domain.Instrument
}
type News struct {
	cfg  NewsConfig
	http *readClient
}

func NewNews(client *http.Client, cfg NewsConfig) (*News, error) {
	if !cfg.Enabled {
		return &News{cfg: cfg}, nil
	}
	if !validBase(cfg.BaseURL) || cfg.Key == "" || cfg.Secret == "" || cfg.Timeout <= 0 {
		return nil, domain.ErrDataNotConfigured
	}
	return &News{cfg: cfg, http: newReadClient(client, cfg.Key, cfg.Secret, cfg.Timeout)}, nil
}
func (n *News) Items(ctx context.Context, ids []string, from, to time.Time) ([]domain.NewsItem, error) {
	if !n.cfg.Enabled {
		return nil, domain.ErrDataNotConfigured
	}
	if len(ids) > 100 || !to.After(from) {
		return nil, domain.ErrInvalidInput
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	bySymbol := map[string]string{}
	var symbols []string
	for _, i := range n.cfg.Instruments {
		if len(wanted) == 0 || wanted[i.ID] {
			if prior := bySymbol[i.Ticker]; prior != "" && prior != i.ID {
				return nil, domain.ErrVersionConflict
			}
			bySymbol[i.Ticker] = i.ID
			symbols = append(symbols, i.Ticker)
		}
	}
	if len(ids) > 0 && len(bySymbol) != len(wanted) {
		return nil, domain.ErrNotFound
	}
	sort.Strings(symbols)
	query := url.Values{"start": {from.Format(time.RFC3339)}, "end": {to.Format(time.RFC3339)}, "include_content": {"false"}, "limit": {"50"}, "sort": {"asc"}}
	if len(symbols) > 0 {
		query.Set("symbols", strings.Join(symbols, ","))
	}
	out := []domain.NewsItem{}
	seen := map[string]bool{}
	tokens := map[string]bool{}
	for page := 0; page < 100; page++ {
		var response struct {
			News []struct {
				ID                             json.Number
				Headline, Summary, URL, Source string
				Symbols                        []string
				CreatedAt                      time.Time `json:"created_at"`
				UpdatedAt                      time.Time `json:"updated_at"`
			}
			NextPageToken string `json:"next_page_token"`
		}
		if e := n.http.get(ctx, n.cfg.BaseURL, "/v1beta1/news", query, &response); e != nil {
			return nil, e
		}
		for _, a := range response.News {
			id := a.ID.String()
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			u, e := url.Parse(a.URL)
			if e != nil || u.User != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
				continue
			}
			available := a.CreatedAt
			if a.UpdatedAt.After(available) {
				available = a.UpdatedAt
			}
			if available.After(to) || a.CreatedAt.Before(from) || a.CreatedAt.IsZero() {
				continue
			}
			links := []string{}
			for _, s := range a.Symbols {
				if instrument := bySymbol[s]; instrument != "" {
					links = append(links, instrument)
				}
			}
			out = append(out, domain.NewsItem{ID: "alpaca:" + id, Title: a.Headline, Summary: a.Summary, URL: a.URL, InstrumentIDs: links, PublishedAt: a.CreatedAt, AvailableAt: available, Provenance: domain.Provenance{Source: a.Source, SourceRecordID: id, IngestedAt: time.Now().UTC()}})
		}
		if response.NextPageToken == "" {
			return out, nil
		}
		if tokens[response.NextPageToken] {
			return nil, &ProviderError{Code: "pagination_loop"}
		}
		tokens[response.NextPageToken] = true
		query.Set("page_token", response.NextPageToken)
	}
	return nil, &ProviderError{Code: "page_limit_exceeded"}
}

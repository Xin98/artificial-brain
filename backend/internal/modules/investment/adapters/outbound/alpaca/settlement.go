package alpaca

import (
	"encoding/json"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"os"
	"sort"
	"strings"
	"time"
)

func LoadSettlementCalendar(path string) (domain.Calendar, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return domain.Calendar{}, domain.ErrDataNotConfigured
	}
	var manifest struct {
		Source  string   `json:"source"`
		Version string   `json:"version"`
		Years   []int    `json:"years"`
		Days    []string `json:"days"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&manifest); e != nil {
		return domain.Calendar{}, domain.ErrInvalidInput
	}
	if strings.TrimSpace(manifest.Source) == "" || manifest.Version == "" || len(manifest.Years) == 0 || len(manifest.Days) == 0 {
		return domain.Calendar{}, domain.ErrInvalidInput
	}
	years := map[int]bool{}
	for _, year := range manifest.Years {
		if year < 2000 || year > 2100 || years[year] {
			return domain.Calendar{}, domain.ErrInvalidInput
		}
		years[year] = true
	}
	sort.Ints(manifest.Years)
	for i := 1; i < len(manifest.Years); i++ {
		if manifest.Years[i] != manifest.Years[i-1]+1 {
			return domain.Calendar{}, domain.ErrInvalidInput
		}
	}
	c := domain.Calendar{Version: manifest.Version, Source: manifest.Source, CoverageStart: time.Date(manifest.Years[0], 1, 1, 0, 0, 0, 0, time.UTC), CoverageEnd: time.Date(manifest.Years[len(manifest.Years)-1]+1, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Nanosecond)}
	seen := map[string]bool{}
	for _, raw := range manifest.Days {
		d, e := time.Parse("2006-01-02", raw)
		if e != nil || !years[d.Year()] || seen[raw] {
			return domain.Calendar{}, domain.ErrInvalidInput
		}
		seen[raw] = true
		c.SettlementDays = append(c.SettlementDays, d)
	}
	sort.Slice(c.SettlementDays, func(i, j int) bool { return c.SettlementDays[i].Before(c.SettlementDays[j]) })
	return c, nil
}

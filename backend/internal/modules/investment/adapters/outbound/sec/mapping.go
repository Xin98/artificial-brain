package sec

import (
	"bytes"
	"encoding/json"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"math/big"
	"sort"
	"strings"
	"time"
)

type conceptMapping struct{ Taxonomy, Tag, Concept, Unit string }

var mappings = []conceptMapping{
	{"us-gaap", "NetIncomeLoss", "NetIncome", "USD"},
	{"us-gaap", "RevenueFromContractWithCustomerExcludingAssessedTax", "Revenue", "USD"},
	{"us-gaap", "Revenues", "Revenue", "USD"}, {"us-gaap", "SalesRevenueNet", "Revenue", "USD"},
	{"us-gaap", "NetCashProvidedByUsedInOperatingActivities", "OperatingCashFlow", "USD"},
	{"us-gaap", "PaymentsToAcquirePropertyPlantAndEquipment", "CapitalExpenditure", "USD"},
	{"us-gaap", "EarningsPerShareDiluted", "DilutedEPS", "USD/shares"},
	{"us-gaap", "StockholdersEquity", "Equity", "USD"}, {"us-gaap", "Assets", "Assets", "USD"},
	{"us-gaap", "Liabilities", "Debt", "USD"},
	{"us-gaap", "CommonStockSharesOutstanding", "Shares", "shares"}, {"dei", "EntityCommonStockSharesOutstanding", "Shares", "shares"},
}

type rawFact struct {
	Start, End, Accn, Filed string
	Val                     json.Number
}

func ParseCompanyFacts(b []byte, instrument string, publication map[string]time.Time, asOf time.Time) ([]domain.FinancialFact, error) {
	var input struct {
		Facts map[string]map[string]struct{ Units map[string][]rawFact }
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if e := dec.Decode(&input); e != nil {
		return nil, &ProviderError{"invalid_response"}
	}
	unique := map[string]domain.FinancialFact{}
	ambiguous := map[string]bool{}
	for _, m := range mappings {
		tag, ok := input.Facts[m.Taxonomy][m.Tag]
		if !ok {
			continue
		}
		for _, raw := range tag.Units[m.Unit] {
			available, ok := publication[raw.Accn]
			if !ok || available.After(asOf) {
				continue
			}
			end, e := time.Parse("2006-01-02", raw.End)
			if e != nil {
				continue
			}
			var start time.Time
			if raw.Start != "" {
				start, e = time.Parse("2006-01-02", raw.Start)
				if e != nil || start.After(end) {
					continue
				}
			}
			number, ok := new(big.Rat).SetString(raw.Val.String())
			if !ok {
				continue
			}
			// JSON decimals have a terminating rational expansion. Preserve the full value.
			den := new(big.Int).Set(number.Denom())
			scale := 0
			for _, prime := range []int64{2, 5} {
				count := 0
				p := big.NewInt(prime)
				for new(big.Int).Mod(den, p).Sign() == 0 {
					den.Div(den, p)
					count++
					if count > 100 {
						return nil, domain.ErrInvalidInput
					}
				}
				if count > scale {
					scale = count
				}
			}
			if den.Cmp(big.NewInt(1)) != 0 || len(number.Num().String()) > 200 {
				return nil, domain.ErrInvalidInput
			}
			value := number.FloatString(scale)
			if scale > 0 {
				value = strings.TrimRight(strings.TrimRight(value, "0"), ".")
			}
			currency := "USD"
			if m.Unit == "shares" {
				currency = ""
			}
			fact := domain.FinancialFact{InstrumentID: instrument, Accession: raw.Accn, Concept: m.Concept, Value: value, Unit: m.Unit, Currency: currency, PeriodStart: start, PeriodEnd: end, AvailableAt: available, Provenance: domain.Provenance{Source: "sec-companyfacts", SourceRecordID: raw.Accn + "/" + m.Taxonomy + "/" + m.Tag + "/" + raw.Start + "/" + raw.End, IngestedAt: time.Now().UTC()}}
			key := raw.Accn + "/" + m.Concept + "/" + raw.Start + "/" + raw.End
			if prior, ok := unique[key]; ok && prior.Value != value {
				ambiguous[key] = true
			} else if !ok {
				unique[key] = fact
			}
		}
	}
	var out []domain.FinancialFact
	for key, f := range unique {
		if !ambiguous[key] {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].AvailableAt.Equal(out[j].AvailableAt) {
			return out[i].AvailableAt.Before(out[j].AvailableAt)
		}
		return out[i].SourceRecordID < out[j].SourceRecordID
	})
	return out, nil
}

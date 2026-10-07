package fixture

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

func (a *Adapter) Facts(ctx context.Context, ciks []string, asOf time.Time) ([]domain.FinancialFact, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	ids := map[string]bool{}
	for _, cik := range ciks {
		for _, i := range a.Snapshot.Instruments {
			if i.CIK == cik {
				ids[i.ID] = true
			}
		}
	}
	var out []domain.FinancialFact
	for _, f := range a.Snapshot.Facts {
		if ids[f.InstrumentID] && !f.AvailableAt.After(asOf) {
			out = append(out, f)
		}
	}
	return out, nil
}
func (a *Adapter) Company(ctx context.Context, cik string) (domain.Instrument, error) {
	if e := ctx.Err(); e != nil {
		return domain.Instrument{}, e
	}
	for _, i := range a.Snapshot.Instruments {
		if i.CIK == cik {
			return i, nil
		}
	}
	return domain.Instrument{}, domain.ErrNotFound
}

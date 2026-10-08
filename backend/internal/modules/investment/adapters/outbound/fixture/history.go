package fixture

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

func (a *Adapter) History(ctx context.Context, mode string, at time.Time) (domain.Snapshot, error) {
	if e := ctx.Err(); e != nil {
		return domain.Snapshot{}, e
	}
	if mode != "fixture" {
		return domain.Snapshot{}, domain.ErrDataNotConfigured
	}
	s := a.Snapshot
	s.AsOf = at
	s.Bars = nil
	s.Facts = nil
	s.Actions = nil
	s.News = nil
	for _, v := range a.Snapshot.Bars {
		if !v.AvailableAt.After(at) {
			s.Bars = append(s.Bars, v)
		}
	}
	for _, v := range a.Snapshot.Facts {
		if !v.AvailableAt.After(at) {
			s.Facts = append(s.Facts, v)
		}
	}
	for _, v := range a.Snapshot.Actions {
		if !v.AvailableAt.After(at) {
			s.Actions = append(s.Actions, v)
		}
	}
	return s, nil
}

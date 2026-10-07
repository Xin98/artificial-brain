package fixture

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

func (a *Adapter) Items(ctx context.Context, ids []string, from, to time.Time) ([]domain.NewsItem, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	out := []domain.NewsItem{}
	for _, n := range a.Snapshot.News {
		if n.PublishedAt.Before(from) || n.AvailableAt.After(to) {
			continue
		}
		match := len(ids) == 0
		for _, id := range n.InstrumentIDs {
			if wanted[id] {
				match = true
			}
		}
		if match {
			out = append(out, n)
		}
	}
	return out, nil
}

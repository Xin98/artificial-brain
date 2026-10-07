package command

import (
	"context"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"strings"
	"time"
)

func DatasetVersion(mode, feed, revision string) string { return mode + "/" + feed + "/" + revision }

type SyncMarketHandler struct {
	Market                     ports.MarketDataPort
	Store                      ports.MarketBatchStore
	UOW                        ports.UnitOfWork
	Mode, Feed, DatasetVersion string
	Now                        func() time.Time
}

func (h SyncMarketHandler) Handle(ctx context.Context, r dto.SyncRequest) (dto.SyncStatus, error) {
	out := dto.SyncStatus{RunID: r.RunID, Mode: h.Mode, DatasetVersion: h.DatasetVersion, State: "failed"}
	if r.Mode != h.Mode || r.DatasetVersion != h.DatasetVersion || len(r.InstrumentIDs) > 100 || !r.To.After(r.From) || r.To.After(h.Now()) {
		return out, domain.ErrInvalidInput
	}
	if !strings.HasPrefix(h.DatasetVersion, h.Mode+"/"+h.Feed+"/") {
		return out, domain.ErrVersionConflict
	}
	instruments, e := h.Market.Instruments(ctx, r.InstrumentIDs)
	if e != nil {
		return out, e
	}
	// Resolve user-entered symbols at the synchronization boundary; all stored economics use stable asset IDs.
	ids := []string{}
	for _, i := range instruments {
		ids = append(ids, i.ID)
	}
	bars, e := h.Market.Bars(ctx, ids, r.From, r.To)
	if e != nil {
		return out, e
	}
	calendar, e := h.Market.Calendar(ctx, r.From, r.To.AddDate(1, 0, 0))
	if e != nil {
		return out, e
	}
	actions, e := h.Market.Actions(ctx, ids, r.From, r.To.AddDate(0, 0, 7))
	if e != nil {
		return out, e
	}
	now := h.Now()
	quality := []string{}
	if h.Mode == "fixture" {
		quality = append(quality, "demonstration_data")
	} else {
		quality = append(quality, "corporate_action_publication_time_unknown", "feed_"+h.Feed)
	}
	if len(calendar.SettlementDays) == 0 {
		quality = append(quality, "settlement_calendar_not_configured")
	}
	batch := dto.MarketBatch{Mode: h.Mode, Feed: h.Feed, Source: h.Mode, Instruments: instruments, Bars: bars, Calendar: calendar, Actions: actions, AsOf: now, QualityFlags: quality}
	if e = h.UOW.Run(ctx, func(ctx context.Context) error { return h.Store.AppendMarket(ctx, h.DatasetVersion, batch) }); e != nil {
		return out, fmt.Errorf("persist market sync: %w", e)
	}
	out.State = "completed"
	out.AsOf = now
	return out, nil
}

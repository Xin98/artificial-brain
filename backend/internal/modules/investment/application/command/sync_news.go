package command

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type SyncNewsHandler struct {
	News                 ports.NewsPort
	Store                ports.NewsBatchStore
	UOW                  ports.UnitOfWork
	Mode, DatasetVersion string
	Now                  func() time.Time
}

func (h SyncNewsHandler) Handle(ctx context.Context, r dto.SyncRequest) (dto.SyncStatus, error) {
	out := dto.SyncStatus{RunID: r.RunID, State: "failed", Mode: h.Mode, DatasetVersion: h.DatasetVersion}
	if r.Mode != h.Mode || r.DatasetVersion != h.DatasetVersion || len(r.InstrumentIDs) > 100 || !r.To.After(r.From) || r.To.After(h.Now()) {
		return out, domain.ErrInvalidInput
	}
	items, e := h.News.Items(ctx, r.InstrumentIDs, r.From, r.To)
	state := "available"
	if errors.Is(e, domain.ErrDataNotConfigured) {
		state = "not_configured"
	} else if e != nil {
		state = "unavailable"
	}
	if persistErr := h.UOW.Run(ctx, func(ctx context.Context) error {
		return h.Store.AppendNews(ctx, h.DatasetVersion, items, state, h.Now())
	}); persistErr != nil {
		return out, persistErr
	}
	out.AsOf = h.Now()
	if e != nil {
		out.ErrorCode = "news_" + state
		return out, e
	}
	out.State = "completed"
	return out, nil
}

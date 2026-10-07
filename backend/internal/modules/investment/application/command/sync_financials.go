package command

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type SyncFinancialsHandler struct {
	Financials           ports.FinancialDataPort
	Identity             ports.FinancialIdentityPort
	Data                 ports.ResearchData
	Store                ports.FinancialBatchStore
	Metadata             ports.InstrumentMetadataStore
	UOW                  ports.UnitOfWork
	Mode, DatasetVersion string
	Now                  func() time.Time
}

func (h SyncFinancialsHandler) Handle(ctx context.Context, r dto.SyncRequest) (dto.SyncStatus, error) {
	out := dto.SyncStatus{RunID: r.RunID, State: "failed", Mode: h.Mode, DatasetVersion: h.DatasetVersion}
	if r.Mode != h.Mode || r.DatasetVersion != h.DatasetVersion || len(r.InstrumentIDs) > 100 {
		return out, domain.ErrInvalidInput
	}
	snapshot, e := h.Data.Read(ctx, h.Mode, h.Now())
	if e != nil {
		return out, e
	}
	if snapshot.DatasetVersion != h.DatasetVersion {
		return out, domain.ErrVersionConflict
	}
	wanted := map[string]bool{}
	for _, id := range r.InstrumentIDs {
		wanted[id] = true
	}
	var instruments []domain.Instrument
	for _, i := range snapshot.Instruments {
		if len(wanted) == 0 || wanted[i.ID] {
			instruments = append(instruments, i)
		}
	}
	if h.Identity != nil {
		instruments, e = h.Identity.Identify(ctx, instruments)
		if e != nil {
			return out, e
		}
	}
	var ciks []string
	byCIK := map[string]string{}
	for _, i := range instruments {
		if i.CIK == "" {
			continue
		}
		if prior := byCIK[i.CIK]; prior != "" && prior != i.ID {
			return out, domain.ErrVersionConflict
		}
		byCIK[i.CIK] = i.ID
		ciks = append(ciks, i.CIK)
	}
	facts, e := h.Financials.Facts(ctx, ciks, h.Now())
	if e != nil {
		return out, e
	}
	secIDs := map[string]string{}
	for cik, id := range byCIK {
		company, e := h.Financials.Company(ctx, cik)
		if e != nil {
			return out, e
		}
		secIDs[company.ID] = id
		secIDs[id] = id
	}
	for n, f := range facts {
		id, ok := secIDs[f.InstrumentID]
		if !ok {
			return out, domain.ErrVersionConflict
		}
		facts[n].InstrumentID = id
	}
	e = h.UOW.Run(ctx, func(ctx context.Context) error {
		if h.Metadata != nil {
			if e := h.Metadata.AppendInstruments(ctx, h.DatasetVersion, instruments, h.Now()); e != nil {
				return e
			}
		}
		return h.Store.AppendFinancials(ctx, h.DatasetVersion, facts)
	})
	if e != nil {
		return out, e
	}
	out.State = "completed"
	out.AsOf = h.Now()
	return out, nil
}

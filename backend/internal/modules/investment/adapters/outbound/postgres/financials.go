package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"time"
)

func (s *SnapshotStore) AppendFinancials(ctx context.Context, dataset string, facts []domain.FinancialFact) error {
	if _, e := database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, "select pg_advisory_xact_lock(hashtextextended($1,0))", dataset); e != nil {
		return e
	}
	now := time.Now().UTC()
	snapshot, e := s.Find(ctx, dataset, now)
	if e != nil {
		return e
	}
	snapshot.ID = fmt.Sprintf("financial/%d", now.UnixNano())
	snapshot.AsOf = now
	// Keep every accession in persisted input; SelectSnapshot chooses the known revision only at a decision cutoff.
	snapshot.Facts = append(append([]domain.FinancialFact(nil), snapshot.Facts...), facts...)
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	for _, f := range facts {
		tag, e := exec.Exec(ctx, `insert into investment.financial_facts(dataset_version,instrument_id,accession,concept,value,unit,currency,period_start,period_end,available_at,ingested_at,source,source_record_id) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) on conflict do nothing`, dataset, f.InstrumentID, f.Accession, f.Concept, f.Value, f.Unit, f.Currency, nullTime(f.PeriodStart), f.PeriodEnd, f.AvailableAt, f.IngestedAt, f.Source, f.SourceRecordID)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			var same bool
			e = exec.QueryRow(ctx, `select value=$5 and concept=$6 and unit=$7 and currency=$8 and period_start is not distinct from $9::timestamptz and period_end=$10 and available_at=$11 from investment.financial_facts where dataset_version=$1 and instrument_id=$2 and accession=$3 and source_record_id=$4`, dataset, f.InstrumentID, f.Accession, f.SourceRecordID, f.Value, f.Concept, f.Unit, f.Currency, nullTime(f.PeriodStart), f.PeriodEnd, f.AvailableAt).Scan(&same)
			if e != nil {
				return e
			}
			if !same {
				return domain.ErrVersionConflict
			}
		}
	}
	return s.Insert(ctx, snapshot)
}
func (s *SnapshotStore) AppendInstruments(ctx context.Context, dataset string, items []domain.Instrument, at time.Time) error {
	if _, e := database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, "select pg_advisory_xact_lock(hashtextextended($1,0))", dataset); e != nil {
		return e
	}
	snapshot, e := s.Find(ctx, dataset, at)
	if e != nil {
		return e
	}
	snapshot.ID = fmt.Sprintf("identity/%d", at.UnixNano())
	snapshot.AsOf = at
	byID := map[string]domain.Instrument{}
	for _, i := range snapshot.Instruments {
		byID[i.ID] = i
	}
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	for _, i := range items {
		old, ok := byID[i.ID]
		if !ok {
			return domain.ErrNotFound
		}
		if old.CIK != "" && i.CIK != old.CIK {
			return domain.ErrVersionConflict
		}
		byID[i.ID] = i
		b, e := json.Marshal(i)
		if e != nil {
			return e
		}
		_, e = exec.Exec(ctx, `insert into investment.instrument_facts(dataset_version,instrument_id,source,source_record_id,effective_at,available_at,ingested_at,projection) values($1,$2,$3,$4,$5,$5,$5,$6) on conflict do nothing`, dataset, i.ID, "financial-identity", i.ID+"/"+at.Format(time.RFC3339Nano), at, b)
		if e != nil {
			return e
		}
		_, e = exec.Exec(ctx, `update investment.instruments set cik=$2,sic=$3,kind=$4,name=$5 where id=$1 and (cik='' or cik=$2)`, i.ID, i.CIK, i.SIC, i.Kind, i.Name)
		if e != nil {
			return e
		}
	}
	snapshot.Instruments = nil
	for _, i := range byID {
		snapshot.Instruments = append(snapshot.Instruments, i)
	}
	snapshot, e = domain.SelectSnapshot(snapshot, at)
	if e != nil {
		return e
	}
	return s.Insert(ctx, snapshot)
}

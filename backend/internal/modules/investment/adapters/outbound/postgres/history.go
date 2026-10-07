package postgres

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"time"
)

func (r ResearchReader) History(ctx context.Context, mode string, at time.Time) (domain.Snapshot, error) {
	if mode != r.Mode {
		return domain.Snapshot{}, domain.ErrDataNotConfigured
	}
	s, e := r.Store.Find(ctx, r.DatasetVersion, at)
	if e != nil {
		return s, e
	}
	exec := database.ExecutorFromContextOr(ctx, r.Store.pool)
	rows, e := exec.Query(ctx, `select instrument_id,accession,concept,value,unit,currency,coalesce(period_start,'0001-01-01'::timestamptz),period_end,available_at,ingested_at,source,source_record_id from investment.financial_facts where dataset_version=$1 and available_at<=$2 and ingested_at<=$2 order by instrument_id,accession,source_record_id`, r.DatasetVersion, at)
	if e != nil {
		return s, e
	}
	defer rows.Close()
	s.Facts = []domain.FinancialFact{}
	for rows.Next() {
		var f domain.FinancialFact
		if e = rows.Scan(&f.InstrumentID, &f.Accession, &f.Concept, &f.Value, &f.Unit, &f.Currency, &f.PeriodStart, &f.PeriodEnd, &f.AvailableAt, &f.IngestedAt, &f.Source, &f.SourceRecordID); e != nil {
			return s, e
		}
		s.Facts = append(s.Facts, f)
	}
	if e = rows.Err(); e != nil {
		return s, e
	}
	rows.Close()
	rows, e = exec.Query(ctx, `select instrument_id,session_date,open_price,high_price,low_price,close_price,volume,available_at,ingested_at,source,source_record_id from investment.price_bars where dataset_version=$1 and available_at<=$2 and ingested_at<=$2 order by instrument_id,session_date,ingested_at`, r.DatasetVersion, at)
	if e != nil {
		return s, e
	}
	defer rows.Close()
	s.Bars = []domain.Bar{}
	for rows.Next() {
		var b domain.Bar
		if e = rows.Scan(&b.InstrumentID, &b.SessionDate, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume, &b.AvailableAt, &b.IngestedAt, &b.Source, &b.SourceRecordID); e != nil {
			return s, e
		}
		b.NoOpeningTrade = b.Volume == 0
		s.Bars = append(s.Bars, b)
	}
	return s, rows.Err()
}

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

func (s *SnapshotStore) AppendMarket(ctx context.Context, dataset string, batch dto.MarketBatch) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	if _, e := exec.Exec(ctx, "select pg_advisory_xact_lock(hashtextextended($1,0))", dataset); e != nil {
		return e
	}
	snapshot := domain.Snapshot{ID: fmt.Sprintf("market/%d", batch.AsOf.UnixNano()), DatasetVersion: dataset, AsOf: batch.AsOf, Mode: batch.Mode, Feed: batch.Feed, Calendar: batch.Calendar, Instruments: batch.Instruments, Bars: batch.Bars, Actions: batch.Actions, QualityFlags: batch.QualityFlags}
	old, e := s.Find(ctx, dataset, batch.AsOf)
	if e != nil && !errors.Is(e, domain.ErrNotFound) {
		return e
	}
	if e == nil {
		snapshot.Facts = old.Facts
		snapshot.News = old.News
		snapshot.Bars = append(append([]domain.Bar(nil), old.Bars...), batch.Bars...)
		snapshot.Actions = append(append([]domain.CorporateAction(nil), old.Actions...), batch.Actions...)
		byID := map[string]domain.Instrument{}
		for _, i := range old.Instruments {
			byID[i.ID] = i
		}
		for _, i := range batch.Instruments {
			prior := byID[i.ID]
			if i.CIK == "" {
				i.CIK = prior.CIK
				i.SIC = prior.SIC
				if i.Kind == "unknown" {
					i.Kind = prior.Kind
				}
			}
			byID[i.ID] = i
		}
		snapshot.Instruments = nil
		for _, i := range byID {
			snapshot.Instruments = append(snapshot.Instruments, i)
		}
	}
	snapshot, e = domain.SelectSnapshot(snapshot, batch.AsOf)
	if e != nil {
		return e
	}
	if e = s.Insert(ctx, snapshot); e != nil {
		return e
	}
	if batch.Instruments == nil {
		batch.Instruments = []domain.Instrument{}
	}
	instruments, e := json.Marshal(batch.Instruments)
	if e != nil {
		return e
	}
	_, e = exec.Exec(ctx, `insert into investment.instruments(id,ticker,name,cik,exchange,sic,kind,tradable) select x."ID",x."Ticker",x."Name",x."CIK",x."Exchange",x."SIC",x."Kind",x."Tradable" from jsonb_to_recordset($1::jsonb) as x("ID" text,"Ticker" text,"Name" text,"CIK" text,"Exchange" text,"SIC" text,"Kind" text,"Tradable" boolean) on conflict(id) do update set ticker=excluded.ticker,name=excluded.name,exchange=excluded.exchange,tradable=excluded.tradable`, instruments)
	if e != nil {
		return e
	}
	_, e = exec.Exec(ctx, `insert into investment.instrument_facts(dataset_version,instrument_id,source,source_record_id,effective_at,available_at,ingested_at,projection) select $1,x->>'ID',x->>'Source',(x->>'SourceRecordID')||'/'||$3,$4,$4,(x->>'IngestedAt')::timestamptz,x from jsonb_array_elements($2::jsonb) x on conflict do nothing`, dataset, instruments, batch.AsOf.Format(time.RFC3339Nano), batch.AsOf)
	if e != nil {
		return e
	}
	bars, e := json.Marshal(batch.Bars)
	if e != nil {
		return e
	}
	if len(batch.Bars) > 0 {
		_, e = exec.Exec(ctx, `insert into investment.price_bars(dataset_version,instrument_id,session_date,available_at,ingested_at,source,source_record_id,open_price,high_price,low_price,close_price,volume) select $1,x."InstrumentID",x."SessionDate"::date,x."AvailableAt",x."IngestedAt",x."Source",x."SourceRecordID",x."Open",x."High",x."Low",x."Close",x."Volume" from jsonb_to_recordset($2::jsonb) as x("InstrumentID" text,"SessionDate" timestamptz,"AvailableAt" timestamptz,"IngestedAt" timestamptz,"Source" text,"SourceRecordID" text,"Open" bigint,"High" bigint,"Low" bigint,"Close" bigint,"Volume" bigint) on conflict do nothing`, dataset, bars)
		if e != nil {
			return e
		}
	}
	for _, a := range batch.Actions {
		b, e := json.Marshal(a)
		if e != nil {
			return e
		}
		_, e = exec.Exec(ctx, `insert into investment.corporate_actions(dataset_version,id,instrument_id,kind,currency,effective_at,available_at,pay_at,ingested_at,source,source_record_id,projection) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) on conflict do nothing`, dataset, a.ID, a.InstrumentID, a.Kind, a.Currency, a.EffectiveAt, a.AvailableAt, nullTime(a.PayAt), a.IngestedAt, a.Source, a.SourceRecordID, b)
		if e != nil {
			return e
		}
	}
	return nil
}
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

type SnapshotStore struct{ pool *pgxpool.Pool }

func NewSnapshotStore(p *pgxpool.Pool) *SnapshotStore { return &SnapshotStore{p} }
func (s *SnapshotStore) Insert(ctx context.Context, v domain.Snapshot) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	coverage, e := json.Marshal(v.QualityFlags)
	if e != nil {
		return e
	}
	_, e = exec.Exec(ctx, `insert into investment.datasets(version,mode,feed,source,created_at,coverage) values($1,$2,$3,$4,$5,$6) on conflict do nothing`, v.DatasetVersion, v.Mode, v.Feed, v.Mode, v.AsOf, coverage)
	if e != nil {
		return e
	}
	var mode, feed string
	if e = exec.QueryRow(ctx, `select mode,feed from investment.datasets where version=$1`, v.DatasetVersion).Scan(&mode, &feed); e != nil {
		return e
	}
	if mode != v.Mode || feed != v.Feed {
		return domain.ErrVersionConflict
	}
	tag, e := exec.Exec(ctx, `insert into investment.data_snapshots(id,dataset_version,as_of,projection) values($1,$2,$3,$4) on conflict do nothing`, v.ID, v.DatasetVersion, v.AsOf, b)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		var equal bool
		if e = exec.QueryRow(ctx, `select projection=$3::jsonb from investment.data_snapshots where dataset_version=$1 and id=$2`, v.DatasetVersion, v.ID, b).Scan(&equal); e != nil {
			return e
		}
		if !equal {
			return domain.ErrVersionConflict
		}
	}
	return nil
}
func (s *SnapshotStore) Get(ctx context.Context, dataset, id string) (domain.Snapshot, error) {
	var b []byte
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select projection from investment.data_snapshots where dataset_version=$1 and id=$2`, dataset, id).Scan(&b)
	if errors.Is(e, pgx.ErrNoRows) {
		return domain.Snapshot{}, domain.ErrNotFound
	}
	if e != nil {
		return domain.Snapshot{}, e
	}
	var out domain.Snapshot
	e = json.Unmarshal(b, &out)
	return out, e
}
func (s *SnapshotStore) Find(ctx context.Context, dataset string, asOf time.Time) (domain.Snapshot, error) {
	var id string
	e := database.ExecutorFromContextOr(ctx, s.pool).QueryRow(ctx, `select id from investment.data_snapshots where dataset_version=$1 and as_of<=$2 order by as_of desc,id limit 1`, dataset, asOf).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return domain.Snapshot{}, domain.ErrNotFound
	}
	if e != nil {
		return domain.Snapshot{}, e
	}
	return s.Get(ctx, dataset, id)
}

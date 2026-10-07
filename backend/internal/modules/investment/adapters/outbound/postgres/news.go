package postgres

import (
	"context"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"time"
)

func (s *SnapshotStore) AppendNews(ctx context.Context, dataset string, items []domain.NewsItem, state string, at time.Time) error {
	if _, e := database.ExecutorFromContextOr(ctx, s.pool).Exec(ctx, "select pg_advisory_xact_lock(hashtextextended($1,0))", dataset); e != nil {
		return e
	}
	snapshot, e := s.Find(ctx, dataset, at)
	if e != nil {
		return e
	}
	snapshot.ID = fmt.Sprintf("news/%d", at.UnixNano())
	snapshot.AsOf = at
	snapshot.News = append(append([]domain.NewsItem(nil), snapshot.News...), items...)
	flags := []string{}
	for _, f := range snapshot.QualityFlags {
		if f != "news_unavailable" && f != "news_not_configured" {
			flags = append(flags, f)
		}
	}
	if state != "available" {
		flags = append(flags, "news_"+state)
	}
	snapshot.QualityFlags = flags
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	for _, n := range items {
		if n.InstrumentIDs == nil {
			n.InstrumentIDs = []string{}
		}
		_, e = exec.Exec(ctx, `insert into investment.news_items(dataset_version,id,title,summary,url,instrument_ids,published_at,available_at,ingested_at,source,source_record_id) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) on conflict do nothing`, dataset, n.ID, n.Title, n.Summary, n.URL, n.InstrumentIDs, n.PublishedAt, n.AvailableAt, n.IngestedAt, n.Source, n.SourceRecordID)
		if e != nil {
			return e
		}
	}
	snapshot, e = domain.SelectSnapshot(snapshot, at)
	if e != nil {
		return e
	}
	return s.Insert(ctx, snapshot)
}

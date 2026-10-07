package ports

import (
	"context"
	"encoding/json"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type RequestStore interface {
	Claim(context.Context, domain.Scope, string, string, string) (dto.RequestRecord, error)
	Complete(context.Context, domain.Scope, string, string, json.RawMessage) error
}

type AccountStore interface {
	Insert(context.Context, domain.Account) error
	Get(context.Context, domain.Scope, string) (domain.Account, error)
	Lock(context.Context, domain.Scope, string) (domain.Account, error)
	Save(context.Context, domain.Account, int) error
}
type CatalogStore interface {
	InsertUniverse(context.Context, domain.Scope, domain.UniverseVersion) error
	GetUniverse(context.Context, domain.Scope, string) (domain.UniverseVersion, error)
	InsertStrategy(context.Context, domain.Scope, domain.StrategyVersion) error
	GetStrategy(context.Context, domain.Scope, string) (domain.StrategyVersion, error)
}
type SnapshotStore interface {
	Insert(context.Context, domain.Snapshot) error
	Get(context.Context, string, string) (domain.Snapshot, error)
	Find(context.Context, string, time.Time) (domain.Snapshot, error)
}
type RunStore interface {
	SaveEvaluation(context.Context, domain.Scope, string, domain.Evaluation) error
	GetEvaluation(context.Context, domain.Scope, string) (domain.Evaluation, error)
}
type LedgerStore interface {
	InsertLedger(context.Context, domain.Scope, string, []domain.LedgerEntry) error
}
type BacktestStore interface {
	InsertBacktest(context.Context, domain.Scope, dto.BacktestRecord) error
	LockBacktest(context.Context, domain.Scope, string) (dto.BacktestRecord, error)
	GetBacktest(context.Context, domain.Scope, string) (dto.BacktestRecord, error)
	SaveBacktest(context.Context, domain.Scope, dto.BacktestRecord, int) error
}
type HistoryData interface {
	History(context.Context, string, time.Time) (domain.Snapshot, error)
}

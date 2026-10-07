package ports

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type JobScheduler interface {
	Enqueue(context.Context, dto.InvestmentJobArgs) (int64, error)
}
type WorkAccountStore interface {
	AccountStore
	WorkDue(context.Context, time.Time) ([]dto.AccountRef, error)
}
type EvaluationStore interface {
	RunStore
	ClaimEvaluation(context.Context, domain.Scope, string, time.Time, string, string) (dto.EvaluationClaim, error)
	UpdateEvaluation(context.Context, domain.Scope, string, domain.Evaluation) error
	IssuedOn(context.Context, domain.Scope, string, time.Time) (bool, error)
	SaveNAV(context.Context, domain.Scope, string, domain.NAVPoint, domain.Money, time.Time, []string) error
}
type SyncRunStore interface {
	InsertSync(context.Context, dto.SyncRun) error
	GetSync(context.Context, domain.Scope, string) (dto.SyncRun, error)
	SaveSync(context.Context, dto.SyncRun) error
}

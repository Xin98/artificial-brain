package worker

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/riverqueue/river"
	"time"
)

type JobHandler interface {
	HandleJob(context.Context, dto.InvestmentJobArgs, bool) error
}
type InvestmentWorker struct {
	river.WorkerDefaults[dto.InvestmentJobArgs]
	Handler JobHandler
}

func (w *InvestmentWorker) Work(ctx context.Context, j *river.Job[dto.InvestmentJobArgs]) error {
	return w.Handler.HandleJob(ctx, j.Args, j.Attempt >= 5)
}
func (w *InvestmentWorker) Timeout(*river.Job[dto.InvestmentJobArgs]) time.Duration {
	return 10 * time.Minute
}
func (w *InvestmentWorker) NextRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt >= 8 {
		return 60 * time.Second
	}
	return 500 * time.Millisecond << uint(attempt-1)
}
func (w *InvestmentWorker) NextRetry(j *river.Job[dto.InvestmentJobArgs]) time.Time {
	return time.Now().Add(w.NextRetryDelay(j.Attempt))
}

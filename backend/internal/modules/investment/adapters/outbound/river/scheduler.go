package river

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
	riverqueue "github.com/riverqueue/river"
	"time"
)

var ErrNoAmbientTransaction = errors.New("investment: scheduling requires an ambient pgx transaction")

type Scheduler struct{ Client *riverqueue.Client[pgx.Tx] }

func (s *Scheduler) Enqueue(ctx context.Context, args dto.InvestmentJobArgs) (int64, error) {
	exec, ok := database.ExecutorFromContext(ctx)
	if !ok {
		return 0, ErrNoAmbientTransaction
	}
	tx, ok := exec.(pgx.Tx)
	if !ok {
		return 0, ErrNoAmbientTransaction
	}
	opts := &riverqueue.InsertOpts{Queue: "investment", MaxAttempts: 5}
	if args.JobType == "account" {
		opts.UniqueOpts = riverqueue.UniqueOpts{ByArgs: true, ByPeriod: 15 * time.Minute}
	}
	out, e := s.Client.InsertTx(ctx, tx, args, opts)
	if e != nil {
		return 0, e
	}
	return out.Job.ID, nil
}

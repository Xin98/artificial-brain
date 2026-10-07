package worker

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"testing"
	"time"
)

type handlerStub struct {
	final bool
	calls int
	err   error
}

func (s *handlerStub) HandleJob(_ context.Context, _ dto.InvestmentJobArgs, final bool) error {
	s.final = final
	s.calls++
	return s.err
}
func TestInvestmentWorkerBoundedRecovery(t *testing.T) {
	s := &handlerStub{err: errors.New("temporary")}
	w := &InvestmentWorker{Handler: s}
	if w.NextRetryDelay(1) != 500*time.Millisecond || w.NextRetryDelay(99) != 60*time.Second {
		t.Fatal("unbounded backoff")
	}
	e := w.Work(context.Background(), &river.Job[dto.InvestmentJobArgs]{JobRow: &rivertype.JobRow{Attempt: 5}, Args: dto.InvestmentJobArgs{JobType: "sync"}})
	if e == nil || !s.final || s.calls != 1 {
		t.Fatal(e, s)
	}
}

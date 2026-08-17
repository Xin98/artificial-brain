package systemhealth

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/platform/observability"
	"github.com/Xin98/artificial-brain/backend/internal/platform/workerstatus"
)

type DatabaseProbe interface {
	Ping(context.Context) error
}

type WorkerLeaseReader interface {
	Latest(context.Context) (workerstatus.Lease, error)
}

type Checker struct {
	db       DatabaseProbe
	workers  WorkerLeaseReader
	now      func() time.Time
	leaseTTL time.Duration
}

func NewChecker(db DatabaseProbe, workers WorkerLeaseReader, now func() time.Time, leaseTTL time.Duration) *Checker {
	return &Checker{db: db, workers: workers, now: now, leaseTTL: leaseTTL}
}

func (c *Checker) Check(ctx context.Context) Report {
	checkedAt := c.now().UTC()
	components := map[string]Component{
		"api": {Status: StatusHealthy, CheckedAt: checkedAt},
	}

	components["database"] = Component{Status: StatusHealthy, CheckedAt: checkedAt}
	if c.db.Ping(ctx) != nil {
		components["database"] = Component{Status: StatusUnavailable, CheckedAt: checkedAt, Detail: "database unavailable"}
	}

	components["worker"] = Component{Status: StatusHealthy, CheckedAt: checkedAt}
	lease, err := c.workers.Latest(ctx)
	if err != nil || workerLeaseExpired(checkedAt, lease.LastHeartbeatAt, c.leaseTTL) {
		components["worker"] = Component{Status: StatusUnavailable, CheckedAt: checkedAt, Detail: "worker heartbeat unavailable"}
	}

	status := StatusHealthy
	for _, component := range components {
		if component.Status != StatusHealthy {
			status = StatusDegraded
			break
		}
	}
	return Report{
		Status:        status,
		CheckedAt:     checkedAt,
		CorrelationID: observability.CorrelationID(ctx),
		Components:    components,
	}
}

func workerLeaseExpired(checkedAt, lastHeartbeatAt time.Time, leaseTTL time.Duration) bool {
	age := checkedAt.Sub(lastHeartbeatAt)
	if age < 0 {
		age = 0
	}
	return age > leaseTTL
}

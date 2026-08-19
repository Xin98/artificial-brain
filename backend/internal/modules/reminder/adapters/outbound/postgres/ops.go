package postgres

import (
	"context"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// queueDepthSQL reports one row per reminder queue, left-joined against the
// waiting river_job backlog so empty queues still appear with depth 0. The
// queue and state lists are fixed constants of this adapter (never user
// input), so they are written as literals to keep the river_job_state enum
// comparison unambiguous.
const queueDepthSQL = `
	with queues(queue) as (
		values ('reminder_email'), ('reminder_sms')
	)
	select q.queue,
		count(j.id),
		coalesce(greatest(0, floor(extract(epoch from ($1::timestamptz - min(j.scheduled_at)))))::bigint, 0)
	from queues q
	left join river_job j
		on j.queue = q.queue
		and j.state in ('available', 'scheduled', 'retryable', 'running')
	group by q.queue
	order by q.queue
`

// OpsStore answers the instance-wide reminder operations snapshot from
// PostgreSQL.
type OpsStore struct {
	pool *pgxpool.Pool
}

// NewOpsStore returns an OpsStore bound to pool.
func NewOpsStore(pool *pgxpool.Pool) *OpsStore { return &OpsStore{pool: pool} }

var _ ports.OpsStore = (*OpsStore)(nil)

// ReminderOps computes the instance-wide snapshot: one row per reminder queue
// (even when empty), delivery lifecycle counts across all workspaces, the
// retry rate, and the p95 submission latency over the trailing window ending
// at now. It is deliberately not workspace-scoped: it is operational data,
// and the ops endpoint is not a tenant read.
func (s *OpsStore) ReminderOps(ctx context.Context, now time.Time, window time.Duration) (dto.OpsView, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)

	queues, err := s.queueDepths(ctx, exec, now)
	if err != nil {
		return dto.OpsView{}, err
	}
	counts, retried, total, err := s.deliveryTotals(ctx, exec)
	if err != nil {
		return dto.OpsView{}, err
	}
	p95Ms, err := s.latencyP95Ms(ctx, exec, now.Add(-window))
	if err != nil {
		return dto.OpsView{}, err
	}

	retryRate := 0.0
	if total > 0 {
		retryRate = float64(retried) / float64(total)
	}
	return dto.OpsView{
		Queues:       queues,
		Deliveries:   counts,
		RetryRate:    retryRate,
		LatencyP95Ms: p95Ms,
		CheckedAt:    now,
	}, nil
}

// queueDepths returns one depth row per reminder queue, in queue-name order.
// oldest_wait_seconds is measured against the injected now so the snapshot is
// reproducible.
func (s *OpsStore) queueDepths(ctx context.Context, exec database.Executor, now time.Time) ([]dto.QueueDepth, error) {
	rows, err := exec.Query(ctx, queueDepthSQL, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var depths []dto.QueueDepth
	for rows.Next() {
		var depth dto.QueueDepth
		if err := rows.Scan(&depth.Queue, &depth.Depth, &depth.OldestWaitSeconds); err != nil {
			return nil, err
		}
		depths = append(depths, depth)
	}
	return depths, rows.Err()
}

// deliveryTotals counts deliveries per lifecycle bucket over all workspaces
// and returns the retry numerator/denominator alongside.
func (s *OpsStore) deliveryTotals(ctx context.Context, exec database.Executor) (dto.DeliveryCounts, int, int, error) {
	var counts dto.DeliveryCounts
	var total, retried int
	err := exec.QueryRow(ctx, `
		select
			count(*) filter (where state = 'scheduled'),
			count(*) filter (where state = 'sending' and attempt_count = 0),
			count(*) filter (where state = 'sending' and attempt_count > 0),
			count(*) filter (where state = 'succeeded'),
			count(*) filter (where state = 'failed'),
			count(*) filter (where state = 'suppressed'),
			count(*),
			count(*) filter (where attempt_count > 0)
		from reminder.reminder_deliveries
	`).Scan(&counts.Scheduled, &counts.Sending, &counts.Retrying, &counts.Succeeded,
		&counts.Failed, &counts.Suppressed, &total, &retried)
	if err != nil {
		return dto.DeliveryCounts{}, 0, 0, err
	}
	return counts, retried, total, nil
}

// latencyP95Ms returns the 95th percentile of (submitted_at - scheduled_at)
// in milliseconds over succeeded deliveries submitted at or after cutoff,
// rounded to the nearest millisecond and 0 when none qualify.
func (s *OpsStore) latencyP95Ms(ctx context.Context, exec database.Executor, cutoff time.Time) (int, error) {
	var p95 float64
	err := exec.QueryRow(ctx, `
		select coalesce(
			percentile_cont(0.95) within group
				(order by extract(epoch from (submitted_at - scheduled_at)) * 1000),
			0)
		from reminder.reminder_deliveries
		where state = 'succeeded' and submitted_at >= $1
	`, cutoff).Scan(&p95)
	if err != nil {
		return 0, err
	}
	return int(math.Round(p95)), nil
}

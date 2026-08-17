package workerstatus

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNoLease = errors.New("worker lease not found")

type Instance struct {
	ID        string
	Version   string
	StartedAt time.Time
}

type Lease struct {
	InstanceID      string
	Version         string
	StartedAt       time.Time
	LastHeartbeatAt time.Time
}

type Registry struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewRegistry(pool *pgxpool.Pool, now func() time.Time) *Registry {
	return &Registry{pool: pool, now: now}
}

func (r *Registry) Record(ctx context.Context, instance Instance) error {
	_, err := r.pool.Exec(ctx, `
		insert into runtime.worker_heartbeats (instance_id, service_version, started_at, last_heartbeat_at)
		values ($1, $2, $3, $4)
		on conflict (instance_id) do update
		set service_version = excluded.service_version,
			last_heartbeat_at = excluded.last_heartbeat_at
	`, instance.ID, instance.Version, instance.StartedAt, r.now())
	return err
}

func (r *Registry) Remove(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, "delete from runtime.worker_heartbeats where instance_id = $1", id)
	return err
}

func (r *Registry) Latest(ctx context.Context) (Lease, error) {
	var lease Lease
	err := r.pool.QueryRow(ctx, `
		select instance_id, service_version, started_at, last_heartbeat_at
		from runtime.worker_heartbeats
		order by last_heartbeat_at desc, instance_id asc
		limit 1
	`).Scan(&lease.InstanceID, &lease.Version, &lease.StartedAt, &lease.LastHeartbeatAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Lease{}, ErrNoLease
	}
	if err != nil {
		return Lease{}, err
	}
	return lease, nil
}

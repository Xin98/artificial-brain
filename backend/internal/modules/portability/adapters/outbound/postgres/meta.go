package postgres

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// instanceIDKey is the public.instance_meta key holding this instance's
// stable identity (D2).
const instanceIDKey = "instance_id"

// MetaStore implements the instance identity seam on public.instance_meta.
type MetaStore struct {
	pool *pgxpool.Pool
}

// NewMetaStore returns a MetaStore bound to pool.
func NewMetaStore(pool *pgxpool.Pool) *MetaStore { return &MetaStore{pool: pool} }

var _ ports.InstanceIdentityStore = (*MetaStore)(nil)

// InstanceID returns this instance's stable id, creating it on first use.
// The insert-on-conflict-do-nothing then select pair is concurrency-safe:
// racing callers each attempt their own generated uuid, exactly one insert
// wins the key, and every caller then reads the winning row back — so any
// number of simultaneous callers converge on the same id.
func (s *MetaStore) InstanceID(ctx context.Context) (string, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	candidate, err := newUUID()
	if err != nil {
		return "", err
	}
	if _, err := exec.Exec(ctx, `
		insert into public.instance_meta (key, value, created_at)
		values ($1, $2, now())
		on conflict (key) do nothing
	`, instanceIDKey, candidate); err != nil {
		return "", err
	}
	var value string
	if err := exec.QueryRow(ctx, `
		select value from public.instance_meta where key = $1
	`, instanceIDKey).Scan(&value); err != nil {
		return "", err
	}
	return value, nil
}

// newUUID returns a fresh random RFC 4122 version-4 uuid string, formatted
// 8-4-4-4-12 with the version and variant bits set.
func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

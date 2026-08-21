package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// uniqueViolation is the PostgreSQL error code for a unique-constraint breach.
const uniqueViolation = "23505"

// SourceRecordStore persists the Source Identity seam in PostgreSQL: which
// bundle records were already imported from which instance, under which
// content fingerprint, and which row in this instance they became.
type SourceRecordStore struct {
	pool *pgxpool.Pool
}

// NewSourceRecordStore returns a SourceRecordStore bound to pool.
func NewSourceRecordStore(pool *pgxpool.Pool) *SourceRecordStore {
	return &SourceRecordStore{pool: pool}
}

var _ ports.SourceRecordStore = (*SourceRecordStore)(nil)

// Fingerprints returns the stored content fingerprints of the given source
// record ids, keyed "sourceInstanceID:sourceRecordID"; ids never imported are
// absent from the map. The lookup is instance-global by design (§7.3) —
// re-import detection keys on (source instance, source record) alone, never on
// workspace — mirroring the reminder module's D6 provider-keyed read.
func (s *SourceRecordStore) Fingerprints(ctx context.Context, sourceInstanceID string, ids []string) (map[string]string, error) {
	return s.lookup(ctx, "content_fingerprint", sourceInstanceID, ids)
}

// Targets returns the stored target ids of the given source record ids, keyed
// like Fingerprints with the target row's id as value.
func (s *SourceRecordStore) Targets(ctx context.Context, sourceInstanceID string, ids []string) (map[string]string, error) {
	return s.lookup(ctx, "target_id", sourceInstanceID, ids)
}

// lookup selects one column of the source records for the given instance and
// record ids, keyed "sourceInstanceID:sourceRecordID".
func (s *SourceRecordStore) lookup(ctx context.Context, column, sourceInstanceID string, ids []string) (map[string]string, error) {
	values := map[string]string{}
	if len(ids) == 0 {
		return values, nil
	}
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	rows, err := exec.Query(ctx, `
		select source_record_id, `+column+`
		from portability.portability_source_records
		where source_instance_id = $1 and source_record_id = any($2)
	`, sourceInstanceID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var sourceRecordID, value string
		if err := rows.Scan(&sourceRecordID, &value); err != nil {
			return nil, err
		}
		values[sourceRecordKey(sourceInstanceID, sourceRecordID)] = value
	}
	return values, rows.Err()
}

// sourceRecordKey builds the map key shared by Fingerprints and Targets:
// "sourceInstanceID:sourceRecordID".
func sourceRecordKey(sourceInstanceID, sourceRecordID string) string {
	return sourceInstanceID + ":" + sourceRecordID
}

// Register records one imported record, mapping any unique violation on
// (source_instance_id, source_record_id) to domain.ErrSourceRecordExists so
// re-importing the same bundle classifies the record instead of copying it.
func (s *SourceRecordStore) Register(ctx context.Context, record dto.SourceRecord) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		insert into portability.portability_source_records
			(workspace_id, source_instance_id, source_record_id, target_kind, target_id, content_fingerprint)
		values ($1, $2, $3, $4, $5, $6)
	`, record.WorkspaceID, record.SourceInstanceID, record.SourceRecordID,
		record.TargetKind, record.TargetID, record.ContentFingerprint)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrSourceRecordExists
	}
	return err
}

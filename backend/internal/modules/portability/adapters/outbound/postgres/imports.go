// Package postgres hosts the Portability module's outbound PostgreSQL
// adapters: the two-phase import lifecycle, the Source Identity seam, and the
// instance identity meta store.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// ImportStore persists the two-phase import lifecycle in PostgreSQL: the
// uploaded bundle row (state pending) and the confirm transition with its
// final report.
type ImportStore struct {
	pool *pgxpool.Pool
}

// NewImportStore returns an ImportStore bound to pool.
func NewImportStore(pool *pgxpool.Pool) *ImportStore { return &ImportStore{pool: pool} }

var _ ports.ImportStore = (*ImportStore)(nil)

// Save inserts an import row: the bundle bytes verbatim, the upload-time
// preview as jsonb when present, and a NULL report until commit.
func (s *ImportStore) Save(ctx context.Context, imp dto.ImportRecordRow) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	var previewArg any
	if imp.Preview != nil {
		previewJSON, err := json.Marshal(imp.Preview)
		if err != nil {
			return err
		}
		previewArg = previewJSON
	}
	var reportArg any
	if imp.Report != nil {
		reportJSON, err := json.Marshal(imp.Report)
		if err != nil {
			return err
		}
		reportArg = reportJSON
	}
	_, err := exec.Exec(ctx, `
		insert into portability.portability_imports
			(id, workspace_id, state, source_instance_id, bundle, preview, report, created_at, committed_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, imp.ID, imp.WorkspaceID, imp.State, imp.SourceInstanceID, imp.Bundle,
		previewArg, reportArg, imp.CreatedAt, imp.CommittedAt)
	return err
}

// Get loads one import row scoped by workspace; a missing row maps to
// domain.ErrImportNotFound.
func (s *ImportStore) Get(ctx context.Context, workspaceID, importID string) (dto.ImportRecordRow, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	row := exec.QueryRow(ctx, `
		select id, workspace_id, state, source_instance_id, bundle, preview, report, created_at, committed_at
		from portability.portability_imports
		where workspace_id = $1 and id = $2
	`, workspaceID, importID)
	var imp dto.ImportRecordRow
	var previewJSON, reportJSON []byte
	err := row.Scan(&imp.ID, &imp.WorkspaceID, &imp.State, &imp.SourceInstanceID,
		&imp.Bundle, &previewJSON, &reportJSON, &imp.CreatedAt, &imp.CommittedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return dto.ImportRecordRow{}, domain.ErrImportNotFound
	}
	if err != nil {
		return dto.ImportRecordRow{}, err
	}
	if previewJSON != nil {
		var preview dto.Preview
		if err := json.Unmarshal(previewJSON, &preview); err != nil {
			return dto.ImportRecordRow{}, err
		}
		imp.Preview = &preview
	}
	if reportJSON != nil {
		var report dto.ImportReport
		if err := json.Unmarshal(reportJSON, &report); err != nil {
			return dto.ImportRecordRow{}, err
		}
		imp.Report = &report
	}
	// pgx scans timestamptz in the local location; the domain works in UTC,
	// so normalize every scanned instant before handing it back.
	imp.CreatedAt = imp.CreatedAt.UTC()
	imp.CommittedAt = utcPtr(imp.CommittedAt)
	return imp, nil
}

// Commit flips a pending row to committed with the report and commit time.
// The update only matches rows still pending, so a second commit — or a commit
// racing an expiry sweep — touches zero rows and reports
// domain.ErrImportConflict.
func (s *ImportStore) Commit(ctx context.Context, workspaceID, importID string, report dto.ImportReport, now time.Time) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return err
	}
	tag, err := exec.Exec(ctx, `
		update portability.portability_imports
		set state = 'committed', report = $3, committed_at = $4
		where id = $1 and workspace_id = $2 and state = 'pending'
	`, importID, workspaceID, reportJSON, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrImportConflict
	}
	return nil
}

// utcPtr returns a pointer to the UTC normalization of t, or nil.
func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

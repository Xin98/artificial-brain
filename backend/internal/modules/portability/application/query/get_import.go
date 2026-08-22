// Package query holds the Portability application's read-side handlers.
package query

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
)

// GetImportQuery renders one import row for its workspace: the preview
// stored at upload, the report once committed, and the lazy TTL expiry.
type GetImportQuery struct {
	Imports   ports.ImportStore
	Now       func() time.Time
	ImportTTL time.Duration
}

// Handle returns the import view. A pending row created before now-TTL
// renders state expired — lazy evaluation only; the stored row is never
// mutated. Committed rows keep their stored report and commit time.
func (q *GetImportQuery) Handle(ctx context.Context, workspaceID, importID string) (dto.ImportView, error) {
	row, err := q.Imports.Get(ctx, workspaceID, importID)
	if err != nil {
		return dto.ImportView{}, err
	}
	state := row.State
	if state == dto.ImportStatePending && row.CreatedAt.Before(q.Now().Add(-q.ImportTTL)) {
		state = dto.ImportStateExpired
	}
	view := dto.ImportView{
		ImportID:         row.ID,
		State:            state,
		SourceInstanceID: row.SourceInstanceID,
		Report:           row.Report,
		CreatedAt:        row.CreatedAt,
		CommittedAt:      row.CommittedAt,
	}
	if row.Preview != nil {
		view.Preview = *row.Preview
	}
	return view, nil
}

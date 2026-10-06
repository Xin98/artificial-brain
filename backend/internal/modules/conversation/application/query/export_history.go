package query

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
)

type ExportHistoryQuery struct{ Store ports.HistoryExporter }

func (h *ExportHistoryQuery) ExportSessions(ctx context.Context, workspace, user string, offset, limit int) ([]dto.HistorySession, error) {
	return h.Store.ExportSessions(ctx, workspace, user, offset, limit)
}
func (h *ExportHistoryQuery) ExportMessages(ctx context.Context, workspace, user string, offset, limit int) ([]dto.HistoryMessage, error) {
	return h.Store.ExportMessages(ctx, workspace, user, offset, limit)
}

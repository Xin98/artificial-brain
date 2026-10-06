package ports

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
)

type HistoryExporter interface {
	ExportSessions(context.Context, string, string, int, int) ([]dto.HistorySession, error)
	ExportMessages(context.Context, string, string, int, int) ([]dto.HistoryMessage, error)
}

type HistoryImporter interface {
	ImportSession(context.Context, string, string, string, dto.HistorySession) error
	ImportMessage(context.Context, string, string, dto.HistoryMessage) (string, error)
}

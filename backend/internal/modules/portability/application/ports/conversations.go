package ports

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
)

var ErrConversationSessionNotFound = errors.New("portability: restored conversation session not found")

type ConversationExporter interface {
	ExportSessions(context.Context, Principal, int, int) ([]dto.SessionExportRecord, error)
	ExportMessages(context.Context, Principal, int, int) ([]dto.MessageExportRecord, error)
}

type ConversationImporter interface {
	ImportSession(context.Context, Principal, dto.SessionImportRequest) (string, error)
	ImportMessage(context.Context, Principal, dto.MessageImportRequest) (string, error)
}

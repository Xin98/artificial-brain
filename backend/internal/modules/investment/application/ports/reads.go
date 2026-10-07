package ports

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
)

type ReadStore interface {
	ListRows(context.Context, dto.ListRequest, dto.PageCursor) ([]dto.ListRow, error)
	LatestNAV(context.Context, domain.Scope, string) (*dto.RecordedNAV, error)
	NAVHistory(context.Context, domain.Scope, string) ([]dto.RecordedNAV, error)
	PerformanceFills(context.Context, domain.Scope, string) ([]domain.Fill, error)
}

package ports

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
)

// SourceSync runs read-only providers outside any account or queue transaction.
type SourceSync interface {
	Sync(context.Context, dto.SyncRequest) error
}

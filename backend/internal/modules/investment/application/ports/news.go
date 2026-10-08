package ports

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type NewsPort interface {
	Items(context.Context, []string, time.Time, time.Time) ([]domain.NewsItem, error)
}
type NewsBatchStore interface {
	AppendNews(context.Context, string, []domain.NewsItem, string, time.Time) error
}

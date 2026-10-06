package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
)

type completedFilterExecutor struct {
	database.Executor
	query string
	args  []any
}

func (e *completedFilterExecutor) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	e.query, e.args = query, args
	return nil, errors.New("query captured")
}

func TestCompletedSinceIsScopedSQLPredicateBeforeLimit(t *testing.T) {
	e := &completedFilterExecutor{}
	ctx := database.WithExecutor(context.Background(), e)
	since := testNow.Add(-7 * 24 * time.Hour)
	_, _ = NewStore(nil).List(ctx, "ws-1", "user-1", dto.ListFilters{Status: "completed", CompletedSince: &since}, 200)
	where, limit := strings.Index(e.query, "where"), strings.Index(e.query, "limit")
	if where < 0 || limit < where || !strings.Contains(e.query[where:limit], "completed_at >= $4") {
		t.Fatalf("completion filter missing before limit: %s", e.query)
	}
	if len(e.args) != 5 || e.args[0] != "ws-1" || e.args[1] != "user-1" || e.args[2] != "completed" || !e.args[3].(time.Time).Equal(since) || e.args[4] != 200 {
		t.Fatalf("filter args = %#v", e.args)
	}
}

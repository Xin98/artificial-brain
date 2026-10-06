package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
)

type enabledWriteExecutor struct {
	database.Executor
	query string
	args  []any
}
type missingEnabledRow struct{}

func (missingEnabledRow) Scan(...any) error { return pgx.ErrNoRows }
func (e *enabledWriteExecutor) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	e.query, e.args = query, args
	return missingEnabledRow{}
}

func TestChannelEnabledWriteChangesOnlyFlagAndScopesOwner(t *testing.T) {
	e := &enabledWriteExecutor{}
	ctx := database.WithExecutor(context.Background(), e)
	_, err := NewChannelStore(nil).SetEnabled(ctx, "workspace", "owner", "channel", false)
	if !errors.Is(err, domain.ErrChannelNotFound) {
		t.Fatalf("scoped missing error=%v", err)
	}
	setStart, where := strings.Index(e.query, "set"), strings.Index(e.query, "where")
	if setStart < 0 || where < setStart || strings.TrimSpace(e.query[setStart:where]) != "set enabled = $4" {
		t.Fatalf("toggle changes verification state: %s", e.query)
	}
	if !strings.Contains(e.query, "id = $1 and user_id = $2 and workspace_id = $3") || len(e.args) != 4 || e.args[0] != "channel" || e.args[1] != "owner" || e.args[2] != "workspace" || e.args[3] != false {
		t.Fatalf("write scope: %s %#v", e.query, e.args)
	}
}

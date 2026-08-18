package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// WorkspaceStore persists personal workspaces in PostgreSQL.
type WorkspaceStore struct {
	pool *pgxpool.Pool
}

func NewWorkspaceStore(pool *pgxpool.Pool) *WorkspaceStore { return &WorkspaceStore{pool: pool} }

func (s *WorkspaceStore) Save(ctx context.Context, workspace domain.PersonalWorkspace) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		insert into identity.workspaces (id, created_at)
		values ($1, $2)
	`, workspace.ID, workspace.CreatedAt)
	return err
}

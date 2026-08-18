package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// UserStore persists users in PostgreSQL.
type UserStore struct {
	pool *pgxpool.Pool
}

func NewUserStore(pool *pgxpool.Pool) *UserStore { return &UserStore{pool: pool} }

func (s *UserStore) Save(ctx context.Context, user domain.User) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		insert into identity.users (id, workspace_id, phone, created_at)
		values ($1, $2, $3, $4)
	`, user.ID, user.WorkspaceID, user.Phone, user.CreatedAt)
	return err
}

func (s *UserStore) ByPhone(ctx context.Context, phone string) (domain.User, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	var user domain.User
	err := exec.QueryRow(ctx, `
		select id, workspace_id, phone, created_at
		from identity.users
		where phone = $1
	`, phone).Scan(&user.ID, &user.WorkspaceID, &user.Phone, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrUserNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	return user, nil
}

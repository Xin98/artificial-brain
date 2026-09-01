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
		insert into identity.users (id, workspace_id, phone, email, created_at)
		values ($1, $2, $3, $4, $5)
	`, user.ID, user.WorkspaceID, nullIfEmpty(user.Phone), nullIfEmpty(user.Email), user.CreatedAt)
	return err
}

func (s *UserStore) ByPhone(ctx context.Context, phone string) (domain.User, error) {
	return s.byIdentifier(ctx, "where phone = $1", phone)
}

func (s *UserStore) ByEmail(ctx context.Context, email string) (domain.User, error) {
	return s.byIdentifier(ctx, "where email = $1", email)
}

func (s *UserStore) byIdentifier(ctx context.Context, where, value string) (domain.User, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	var user domain.User
	var phone, email *string
	err := exec.QueryRow(ctx, `
		select id, workspace_id, phone, email, created_at
		from identity.users
		`+where, value).Scan(&user.ID, &user.WorkspaceID, &phone, &email, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrUserNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	user.Phone = stringValue(phone)
	user.Email = stringValue(email)
	return user, nil
}

// nullIfEmpty renders an absent identifier as SQL NULL so unique constraints
// and the "at least one identifier" check behave correctly.
func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

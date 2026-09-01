package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

// ChallengeStore persists login challenges in PostgreSQL.
type ChallengeStore struct {
	pool *pgxpool.Pool
}

func NewChallengeStore(pool *pgxpool.Pool) *ChallengeStore { return &ChallengeStore{pool: pool} }

func (s *ChallengeStore) Save(ctx context.Context, challenge domain.LoginChallenge) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		insert into identity.login_challenges
			(id, phone, email, code_hash, created_at, expires_at, consumed_at, attempts)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
	`, challenge.ID, nullIfEmpty(challenge.Phone), nullIfEmpty(challenge.Email), challenge.CodeHash,
		challenge.CreatedAt, challenge.ExpiresAt, challenge.ConsumedAt, challenge.Attempts)
	return err
}

func (s *ChallengeStore) Update(ctx context.Context, challenge domain.LoginChallenge) error {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	_, err := exec.Exec(ctx, `
		update identity.login_challenges
		set code_hash = $2, expires_at = $3, consumed_at = $4, attempts = $5
		where id = $1
	`, challenge.ID, challenge.CodeHash, challenge.ExpiresAt, challenge.ConsumedAt, challenge.Attempts)
	return err
}

// ActiveByPhone returns the most recent unconsumed challenge for the phone,
// falling back to the most recent challenge overall so callers can
// distinguish consumed and expired states.
func (s *ChallengeStore) ActiveByPhone(ctx context.Context, phone string) (domain.LoginChallenge, error) {
	return s.activeByIdentifier(ctx, "where phone = $1", phone)
}

// ActiveByEmail mirrors ActiveByPhone for email identifiers.
func (s *ChallengeStore) ActiveByEmail(ctx context.Context, email string) (domain.LoginChallenge, error) {
	return s.activeByIdentifier(ctx, "where email = $1", email)
}

func (s *ChallengeStore) activeByIdentifier(ctx context.Context, where, value string) (domain.LoginChallenge, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	var challenge domain.LoginChallenge
	var phone, email *string
	err := exec.QueryRow(ctx, `
		select id, phone, email, code_hash, created_at, expires_at, consumed_at, attempts
		from identity.login_challenges
		`+where+`
		order by (consumed_at is null) desc, created_at desc
		limit 1
	`, value).Scan(
		&challenge.ID, &phone, &email, &challenge.CodeHash, &challenge.CreatedAt,
		&challenge.ExpiresAt, &challenge.ConsumedAt, &challenge.Attempts,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LoginChallenge{}, domain.ErrChallengeNotFound
	}
	if err != nil {
		return domain.LoginChallenge{}, err
	}
	challenge.Phone = stringValue(phone)
	challenge.Email = stringValue(email)
	return challenge, nil
}

func (s *ChallengeStore) CountByPhoneSince(ctx context.Context, phone string, since time.Time) (int, error) {
	return s.countByIdentifierSince(ctx, "where phone = $1 and created_at >= $2", phone, since)
}

func (s *ChallengeStore) CountByEmailSince(ctx context.Context, email string, since time.Time) (int, error) {
	return s.countByIdentifierSince(ctx, "where email = $1 and created_at >= $2", email, since)
}

func (s *ChallengeStore) countByIdentifierSince(ctx context.Context, where, value string, since time.Time) (int, error) {
	exec := database.ExecutorFromContextOr(ctx, s.pool)
	var count int
	if err := exec.QueryRow(ctx, `
		select count(*)
		from identity.login_challenges
		`+where, value, since).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

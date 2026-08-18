package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const CurrentSchemaVersion int32 = 5

var ErrSchemaIncompatible = errors.New("database schema is incompatible")

type Queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func RequireSchema(ctx context.Context, q Queryer, expected int32) error {
	var version int32
	if err := q.QueryRow(ctx, "select version from public.schema_version limit 1").Scan(&version); err != nil {
		return fmt.Errorf("%w: schema version is unavailable", ErrSchemaIncompatible)
	}
	if version != expected {
		return fmt.Errorf("%w: expected version %d", ErrSchemaIncompatible, expected)
	}
	return nil
}

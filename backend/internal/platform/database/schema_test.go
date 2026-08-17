package database

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

const schemaVersionQuery = "select version from public.schema_version limit 1"

func TestRequireSchemaAcceptsExpectedVersion(t *testing.T) {
	q := fakeQueryer{row: fakeRow{version: CurrentSchemaVersion}}

	if err := RequireSchema(context.Background(), &q, CurrentSchemaVersion); err != nil {
		t.Fatalf("RequireSchema() error = %v", err)
	}
	if q.query != schemaVersionQuery {
		t.Fatalf("QueryRow() query = %q, want %q", q.query, schemaVersionQuery)
	}
}

func TestRequireSchemaRejectsMissingSchema(t *testing.T) {
	q := fakeQueryer{row: fakeRow{err: pgx.ErrNoRows}}

	err := RequireSchema(context.Background(), &q, CurrentSchemaVersion)
	if !errors.Is(err, ErrSchemaIncompatible) {
		t.Fatalf("RequireSchema() error = %v, want ErrSchemaIncompatible", err)
	}
}

func TestRequireSchemaRejectsMismatchWithoutLeakingURL(t *testing.T) {
	q := fakeQueryer{row: fakeRow{version: 0}}

	err := RequireSchema(context.Background(), &q, CurrentSchemaVersion)
	if !errors.Is(err, ErrSchemaIncompatible) {
		t.Fatalf("RequireSchema() error = %v, want ErrSchemaIncompatible", err)
	}
	if q.execCalled {
		t.Fatal("schema check attempted a write")
	}
	if got := err.Error(); strings.Contains(got, "postgres://") {
		t.Fatalf("RequireSchema() error leaked database URL: %q", got)
	}
}

type fakeQueryer struct {
	row        pgx.Row
	query      string
	execCalled bool
}

func (q *fakeQueryer) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	q.query = query
	return q.row
}

type fakeRow struct {
	version int32
	err     error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*int32)) = r.version
	return nil
}

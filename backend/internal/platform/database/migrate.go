package database

import (
	"context"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/tern/v2/migrate"
)

func RunMigrations(ctx context.Context, url, directory string) error {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	migrator, err := migrate.NewMigrator(ctx, conn, "public.schema_version")
	if err != nil {
		return err
	}
	if err := migrator.LoadMigrations(os.DirFS(directory)); err != nil {
		return err
	}
	return migrator.Migrate(ctx)
}

package main

import (
	"context"
	"os"

	"github.com/Xin98/artificial-brain/backend/internal/platform/config"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/Xin98/artificial-brain/backend/internal/platform/observability"
)

func main() {
	logger := observability.NewLogger(os.Stderr, "migrate", "unknown")
	cfg, err := config.Load(config.RoleMigrate, os.LookupEnv)
	if err != nil {
		// The runbook depends on operators seeing WHICH variable is missing
		// or invalid, so the config error must reach the log line.
		logger.Error("invalid migration configuration", "error", err)
		os.Exit(1)
	}

	logger = observability.NewLogger(os.Stderr, cfg.ServiceName, cfg.ServiceVersion)
	logger.Info("running migrations", "directory", cfg.MigrationsDir, "schema_version", database.CurrentSchemaVersion)
	if err := database.RunMigrations(context.Background(), cfg.DatabaseURL, cfg.MigrationsDir); err != nil {
		logger.Error("migration failed")
		os.Exit(1)
	}
}

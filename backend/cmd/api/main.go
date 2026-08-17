package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/platform/config"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/Xin98/artificial-brain/backend/internal/platform/observability"
	"github.com/Xin98/artificial-brain/backend/internal/platform/server"
	"github.com/Xin98/artificial-brain/backend/internal/platform/systemhealth"
	"github.com/Xin98/artificial-brain/backend/internal/platform/workerstatus"
)

func main() {
	logger := observability.NewLogger(os.Stderr, "api", "unknown")
	cfg, err := config.Load(config.RoleAPI, os.LookupEnv)
	if err != nil {
		logger.Error("invalid api configuration")
		os.Exit(1)
	}
	logger = observability.NewLogger(os.Stderr, cfg.ServiceName, cfg.ServiceVersion)

	pool, err := database.OpenPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("database startup failed")
		os.Exit(1)
	}
	defer pool.Close()

	ready := func(ctx context.Context) error {
		if err := pool.Ping(ctx); err != nil {
			return err
		}
		return database.RequireSchema(ctx, pool, database.CurrentSchemaVersion)
	}
	workers := workerstatus.NewRegistry(pool, time.Now)
	checker := systemhealth.NewChecker(pool, workers, time.Now, cfg.WorkerLeaseTTL)
	srv := &http.Server{Addr: cfg.HTTPAddress, Handler: server.NewAPIHandler(ready, checker)}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.Serve(ctx, srv, cfg.ShutdownTimeout); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("api server failed")
		os.Exit(1)
	}
}

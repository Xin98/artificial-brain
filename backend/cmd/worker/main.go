package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	"github.com/Xin98/artificial-brain/backend/internal/platform/workerstatus"
)

func main() {
	os.Exit(run())
}

func run() int {
	logger := observability.NewLogger(os.Stderr, "worker", "unknown")
	cfg, err := config.Load(config.RoleWorker, os.LookupEnv)
	if err != nil {
		logger.Error("invalid worker configuration")
		return 1
	}
	logger = observability.NewLogger(os.Stderr, cfg.ServiceName, cfg.ServiceVersion)

	pool, err := database.OpenPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("database startup failed")
		return 1
	}
	defer pool.Close()

	if err := database.RequireSchema(context.Background(), pool, database.CurrentSchemaVersion); err != nil {
		logger.Error("database schema verification failed")
		return 1
	}

	instanceID, err := workerInstanceID()
	if err != nil {
		logger.Error("worker instance ID generation failed")
		return 1
	}
	ticks, err := workerstatus.NewTimeTickSource(cfg.HeartbeatInterval)
	if err != nil {
		logger.Error("worker heartbeat configuration failed")
		return 1
	}

	registry := workerstatus.NewRegistry(pool, time.Now)
	state := &workerstatus.State{}
	heartbeat := workerstatus.NewHeartbeat(stateRecorder{Recorder: registry, state: state}, workerstatus.Instance{
		ID:        instanceID,
		Version:   cfg.ServiceVersion,
		StartedAt: time.Now(),
	}, ticks)
	ready := func(ctx context.Context) error {
		if err := pool.Ping(ctx); err != nil {
			return err
		}
		return database.RequireSchema(ctx, pool, database.CurrentSchemaVersion)
	}
	healthServer := &http.Server{Addr: cfg.HTTPAddress, Handler: server.NewWorkerHealthHandler(ready, state.Ready)}

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()

	errs := make(chan error, 2)
	go func() { errs <- heartbeat.Run(ctx) }()
	go func() { errs <- server.Serve(ctx, healthServer, cfg.ShutdownTimeout) }()

	var failed bool
	for range 2 {
		err := <-errs
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
			failed = true
			cancel()
		}
	}
	if failed {
		logger.Error("worker runtime failed")
		return 1
	}
	return 0
}

type stateRecorder struct {
	workerstatus.Recorder
	state *workerstatus.State
}

func (r stateRecorder) Record(ctx context.Context, instance workerstatus.Instance) error {
	err := r.Recorder.Record(ctx, instance)
	if err != nil {
		r.state.MarkHeartbeatFailure()
		return err
	}
	r.state.MarkHeartbeatSuccess()
	return nil
}

func workerInstanceID() (string, error) {
	if id := os.Getenv("WORKER_INSTANCE_ID"); id != "" {
		return id, nil
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(random[:]), nil
}

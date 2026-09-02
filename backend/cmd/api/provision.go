package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	identitypostgres "github.com/Xin98/artificial-brain/backend/internal/modules/identity/adapters/outbound/postgres"
	identitycommand "github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/command"
	portabilitypostgres "github.com/Xin98/artificial-brain/backend/internal/modules/portability/adapters/outbound/postgres"
	"github.com/Xin98/artificial-brain/backend/internal/platform/config"
)

// instanceIdentityKey mirrors the portability MetaStore's public.instance_meta
// key so provisioning can tell a first creation from a repeat start and log
// the instance identity exactly once.
const instanceIdentityKey = "instance_id"

// provisionInstanceIdentity resolves this instance's stable id through the
// portability MetaStore (get-or-create on public.instance_meta) and logs the
// resolved instance id on first creation.
func provisionInstanceIdentity(ctx context.Context, pool *pgxpool.Pool) error {
	var exists bool
	if err := pool.QueryRow(ctx,
		`select exists(select 1 from public.instance_meta where key = $1)`,
		instanceIdentityKey).Scan(&exists); err != nil {
		return err
	}
	instanceID, err := portabilitypostgres.NewMetaStore(pool).InstanceID(ctx)
	if err != nil {
		return err
	}
	if !exists {
		slog.Info("instance identity established", slog.String("instanceId", instanceID))
	}
	return nil
}

// provisionPrivateAdmin idempotently provisions the fixed private-deployment
// admin workspace+user through Identity's public handler; it is a no-op
// outside the private deployment mode.
func provisionPrivateAdmin(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) error {
	if cfg.DeploymentMode != config.DeploymentModePrivate {
		return nil
	}
	handler := identitycommand.ProvisionAdminHandler{
		Users:      identitypostgres.NewUserStore(pool),
		Workspaces: identitypostgres.NewWorkspaceStore(pool),
		NewID:      newID,
		Now:        time.Now,
	}
	if err := handler.Handle(ctx, cfg.PrivateAdminPhone, cfg.PrivateAdminEmail); err != nil {
		return err
	}
	slog.Info("private admin provisioned")
	return nil
}

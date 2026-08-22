package command

import (
	"context"
	"errors"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// ProvisionAdminHandler idempotently provisions the fixed private-deployment
// admin identity.
type ProvisionAdminHandler struct {
	Users      ports.UserStore
	Workspaces ports.WorkspaceStore
	NewID      func() string
	Now        func() time.Time
}

// Handle idempotently provisions the fixed private admin: an existing user
// for the phone is a no-op; otherwise a fresh personal workspace + user is
// saved (workspace first, mirroring the first-login registration).
func (h *ProvisionAdminHandler) Handle(ctx context.Context, phone string) error {
	p, err := domain.NewPhone(phone)
	if err != nil {
		return err
	}
	_, err = h.Users.ByPhone(ctx, p.String())
	if err == nil {
		return nil
	}
	if !errors.Is(err, domain.ErrUserNotFound) {
		return err
	}
	now := h.Now()
	workspace := domain.PersonalWorkspace{ID: h.NewID(), CreatedAt: now}
	if err := h.Workspaces.Save(ctx, workspace); err != nil {
		return err
	}
	user := domain.User{ID: h.NewID(), WorkspaceID: workspace.ID, Phone: p.String(), CreatedAt: now}
	return h.Users.Save(ctx, user)
}

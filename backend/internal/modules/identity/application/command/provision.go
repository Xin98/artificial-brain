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

// Handle idempotently provisions the fixed private admin. Either identifier
// may be empty; when both are configured they belong to the same admin user.
// An existing user matching any configured identifier is a no-op; otherwise a
// fresh personal workspace + user is saved (workspace first, mirroring the
// first-login registration).
func (h *ProvisionAdminHandler) Handle(ctx context.Context, phone, email string) error {
	if phone == "" && email == "" {
		return domain.ErrIdentifierInvalid
	}
	if phone != "" {
		if _, err := domain.NewPhone(phone); err != nil {
			return err
		}
		if _, err := h.Users.ByPhone(ctx, phone); err == nil {
			return nil
		} else if !errors.Is(err, domain.ErrUserNotFound) {
			return err
		}
	}
	if email != "" {
		if _, err := domain.NewEmail(email); err != nil {
			return err
		}
		if _, err := h.Users.ByEmail(ctx, email); err == nil {
			return nil
		} else if !errors.Is(err, domain.ErrUserNotFound) {
			return err
		}
	}
	now := h.Now()
	workspace := domain.PersonalWorkspace{ID: h.NewID(), CreatedAt: now}
	if err := h.Workspaces.Save(ctx, workspace); err != nil {
		return err
	}
	user := domain.User{ID: h.NewID(), WorkspaceID: workspace.ID, Phone: phone, Email: email, CreatedAt: now}
	return h.Users.Save(ctx, user)
}

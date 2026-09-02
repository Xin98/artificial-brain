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
// An existing user matching any configured identifier is reused, never
// forked: when that user carries only one of two configured identifiers the
// missing one is attached, so a later first-login with the other identifier
// resolves to the same workspace instead of registering a second user. When
// no identifier matches, a fresh personal workspace + user is saved
// (workspace first, mirroring the first-login registration).
func (h *ProvisionAdminHandler) Handle(ctx context.Context, phone, email string) error {
	if phone == "" && email == "" {
		return domain.ErrIdentifierInvalid
	}
	if phone != "" {
		if _, err := domain.NewPhone(phone); err != nil {
			return err
		}
	}
	if email != "" {
		if _, err := domain.NewEmail(email); err != nil {
			return err
		}
	}

	existing, err := h.findExisting(ctx, phone, email)
	if err != nil {
		return err
	}
	if existing != nil {
		changed := false
		if phone != "" && existing.Phone == "" {
			existing.Phone = phone
			changed = true
		}
		if email != "" && existing.Email == "" {
			existing.Email = email
			changed = true
		}
		if !changed {
			return nil
		}
		return h.Users.Update(ctx, *existing)
	}

	now := h.Now()
	workspace := domain.PersonalWorkspace{ID: h.NewID(), CreatedAt: now}
	if err := h.Workspaces.Save(ctx, workspace); err != nil {
		return err
	}
	user := domain.User{ID: h.NewID(), WorkspaceID: workspace.ID, Phone: phone, Email: email, CreatedAt: now}
	return h.Users.Save(ctx, user)
}

// findExisting returns the stored admin user matching either configured
// identifier, or nil when neither exists yet.
func (h *ProvisionAdminHandler) findExisting(ctx context.Context, phone, email string) (*domain.User, error) {
	if phone != "" {
		user, err := h.Users.ByPhone(ctx, phone)
		if err == nil {
			return &user, nil
		}
		if !errors.Is(err, domain.ErrUserNotFound) {
			return nil, err
		}
	}
	if email != "" {
		user, err := h.Users.ByEmail(ctx, email)
		if err == nil {
			return &user, nil
		}
		if !errors.Is(err, domain.ErrUserNotFound) {
			return nil, err
		}
	}
	return nil, nil
}

package command

import (
	"context"
	"errors"
	"testing"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

func newProvisionHandler(users *fakeUserStore, workspaces *fakeWorkspaceStore) *ProvisionAdminHandler {
	gen := &idGenerator{}
	return &ProvisionAdminHandler{Users: users, Workspaces: workspaces, NewID: gen.next, Now: fixedNow}
}

func TestProvisionAdminSavesWorkspaceThenUser(t *testing.T) {
	var order []string
	users := newFakeUserStore()
	users.log = &order
	workspaces := newFakeWorkspaceStore()
	workspaces.log = &order
	h := newProvisionHandler(users, workspaces)
	phone := "+8613800137000"

	if err := h.Handle(context.Background(), phone, ""); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(order) != 2 || order[0] != "workspace:id-1" || order[1] != "user:id-2" {
		t.Fatalf("save order = %v, want [workspace:id-1 user:id-2]", order)
	}
	user, err := users.ByPhone(context.Background(), phone)
	if err != nil {
		t.Fatalf("user not provisioned: %v", err)
	}
	if user.WorkspaceID != "id-1" || user.Phone != phone {
		t.Fatalf("user = %#v, want WorkspaceID id-1 and phone %s", user, phone)
	}
	if len(workspaces.workspaces) != 1 {
		t.Fatalf("workspaces = %d, want 1", len(workspaces.workspaces))
	}
}

func TestProvisionAdminExistingUserIsNoop(t *testing.T) {
	var order []string
	users := newFakeUserStore()
	users.log = &order
	workspaces := newFakeWorkspaceStore()
	workspaces.log = &order
	h := newProvisionHandler(users, workspaces)
	phone := "+8613800137000"

	if err := h.Handle(context.Background(), phone, ""); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), phone, ""); err != nil {
		t.Fatalf("second Handle() error = %v, want idempotent no-op", err)
	}
	if len(order) != 2 {
		t.Fatalf("saves = %v, want exactly the first-run workspace+user", order)
	}
	if len(users.users) != 1 || len(workspaces.workspaces) != 1 {
		t.Fatalf("got %d users, %d workspaces, want 1 of each", len(users.users), len(workspaces.workspaces))
	}
}

func TestProvisionAdminByPhoneErrorPropagates(t *testing.T) {
	var order []string
	users := newFakeUserStore()
	users.log = &order
	users.byPhoneErr = errors.New("database unavailable")
	workspaces := newFakeWorkspaceStore()
	workspaces.log = &order
	h := newProvisionHandler(users, workspaces)

	err := h.Handle(context.Background(), "+8613800137000", "")
	if err == nil || err.Error() != "database unavailable" {
		t.Fatalf("Handle() error = %v, want propagated ByPhone error", err)
	}
	if len(order) != 0 {
		t.Fatalf("saves = %v, want none", order)
	}
}

func TestProvisionAdminRejectsInvalidPhone(t *testing.T) {
	users := newFakeUserStore()
	workspaces := newFakeWorkspaceStore()
	h := newProvisionHandler(users, workspaces)

	if err := h.Handle(context.Background(), "not-a-phone", ""); !errors.Is(err, domain.ErrInvalidPhone) {
		t.Fatalf("Handle() error = %v, want ErrInvalidPhone", err)
	}
	if len(users.users) != 0 || len(workspaces.workspaces) != 0 {
		t.Fatal("invalid phone must not write anything")
	}
}

func TestProvisionAdminBothIdentifiers(t *testing.T) {
	users := newFakeUserStore()
	workspaces := newFakeWorkspaceStore()
	h := newProvisionHandler(users, workspaces)

	if err := h.Handle(context.Background(), "+8613800137999", "admin@example.com"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	user, err := users.ByEmail(context.Background(), "admin@example.com")
	if err != nil {
		t.Fatalf("ByEmail: %v", err)
	}
	if user.Phone != "+8613800137999" {
		t.Fatalf("user = %#v, want both identifiers", user)
	}
	// Idempotent on either identifier.
	if err := h.Handle(context.Background(), "+8613800137999", ""); err != nil {
		t.Fatalf("re-provision phone: %v", err)
	}
	if err := h.Handle(context.Background(), "", "admin@example.com"); err != nil {
		t.Fatalf("re-provision email: %v", err)
	}
	if len(users.users) != 1 || len(workspaces.workspaces) != 1 {
		t.Fatalf("got %d users, %d workspaces, want 1 of each", len(users.users), len(workspaces.workspaces))
	}
}

func TestProvisionAdminNoIdentifier(t *testing.T) {
	h := newProvisionHandler(newFakeUserStore(), newFakeWorkspaceStore())

	if err := h.Handle(context.Background(), "", ""); !errors.Is(err, domain.ErrIdentifierInvalid) {
		t.Fatalf("Handle = %v, want ErrIdentifierInvalid", err)
	}
}

func TestProvisionAdminByEmailErrorPropagates(t *testing.T) {
	var order []string
	users := newFakeUserStore()
	users.log = &order
	users.byEmailErr = errors.New("database unavailable")
	workspaces := newFakeWorkspaceStore()
	workspaces.log = &order
	h := newProvisionHandler(users, workspaces)

	err := h.Handle(context.Background(), "", "admin@example.com")
	if err == nil || err.Error() != "database unavailable" {
		t.Fatalf("Handle() error = %v, want propagated ByEmail error", err)
	}
	if len(order) != 0 {
		t.Fatalf("saves = %v, want none", order)
	}
}

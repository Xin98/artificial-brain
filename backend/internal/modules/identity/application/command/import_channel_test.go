package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

func newImportChannelHandler(channels *fakeChannelStore, now func() time.Time) *ImportChannelHandler {
	gen := &idGenerator{}
	return &ImportChannelHandler{Channels: channels, NewID: gen.next, Now: now}
}

func TestImportChannelCreatesUnverifiedChannel(t *testing.T) {
	channels := newFakeChannelStore()
	h := newImportChannelHandler(channels, fixedNow)

	view, err := h.Handle(context.Background(), testPrincipal, "email", "user@example.com", true)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if view.Verified || !view.Enabled || view.Kind != "email" || view.Address != "user@example.com" {
		t.Fatalf("view = %#v", view)
	}
	channel, err := channels.ByID(context.Background(), testPrincipal.WorkspaceID, testPrincipal.UserID, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if channel.Verified || channel.CodeHash != "" || channel.CodeExpiresAt != nil {
		t.Fatalf("imported channel must be unverified without code state: %#v", channel)
	}
	if !channel.CreatedAt.Equal(testNow) {
		t.Fatalf("CreatedAt = %v, want %v", channel.CreatedAt, testNow)
	}
}

func TestImportChannelHonorsDisabledRequest(t *testing.T) {
	channels := newFakeChannelStore()
	h := newImportChannelHandler(channels, fixedNow)

	view, err := h.Handle(context.Background(), testPrincipal, "sms", "+8613800137001", false)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if view.Enabled {
		t.Fatalf("view = %#v, want disabled", view)
	}
}

func TestImportChannelRejectsInvalidKindAndAddress(t *testing.T) {
	channels := newFakeChannelStore()
	h := newImportChannelHandler(channels, fixedNow)

	if _, err := h.Handle(context.Background(), testPrincipal, "wechat", "user@example.com", true); !errors.Is(err, domain.ErrInvalidChannelKind) {
		t.Fatalf("invalid kind error = %v, want ErrInvalidChannelKind", err)
	}
	if _, err := h.Handle(context.Background(), testPrincipal, "email", "not-an-email", true); !errors.Is(err, domain.ErrInvalidEmail) {
		t.Fatalf("invalid email error = %v, want ErrInvalidEmail", err)
	}
	if _, err := h.Handle(context.Background(), testPrincipal, "sms", "123", true); !errors.Is(err, domain.ErrInvalidPhone) {
		t.Fatalf("invalid sms error = %v, want ErrInvalidPhone", err)
	}
	if len(channels.channels) != 0 {
		t.Fatalf("channels stored = %d, want 0", len(channels.channels))
	}
}

func TestImportChannelDuplicateSurfacesErrChannelExists(t *testing.T) {
	channels := newFakeChannelStore()
	h := newImportChannelHandler(channels, fixedNow)

	if _, err := h.Handle(context.Background(), testPrincipal, "email", "user@example.com", true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Handle(context.Background(), testPrincipal, "email", "user@example.com", false); !errors.Is(err, domain.ErrChannelExists) {
		t.Fatalf("duplicate error = %v, want ErrChannelExists", err)
	}
	if len(channels.channels) != 1 {
		t.Fatalf("channels stored = %d, want 1", len(channels.channels))
	}
}

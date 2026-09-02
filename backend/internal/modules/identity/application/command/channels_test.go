package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

var testPrincipal = dto.Principal{UserID: "u1", WorkspaceID: "w1", SessionID: "s1"}

func newAddChannelHandler(channels *fakeChannelStore, outbox *fakeOutbox, now func() time.Time) *AddChannelHandler {
	gen := &idGenerator{}
	return &AddChannelHandler{
		Channels: channels,
		Outbox:   outbox,
		NewCode:  func() (string, error) { return "222333", nil },
		NewID:    gen.next,
		Now:      now,
		CodeTTL:  10 * time.Minute,
	}
}

func TestAddChannelCreatesUnverifiedChannelAndOutbox(t *testing.T) {
	channels := newFakeChannelStore()
	outbox := &fakeOutbox{}
	h := newAddChannelHandler(channels, outbox, fixedNow)

	view, err := h.Handle(context.Background(), testPrincipal, "email", "user@example.com")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if view.Verified || !view.Enabled || view.Kind != "email" || view.Address != "user@example.com" {
		t.Fatalf("view = %#v", view)
	}
	if len(outbox.messages) != 1 || outbox.messages[0].Purpose != "channel_verification" || outbox.messages[0].Channel != "email" {
		t.Fatalf("outbox = %#v", outbox.messages)
	}
}

func TestAddChannelRejectsInvalidKindAndAddress(t *testing.T) {
	channels := newFakeChannelStore()
	outbox := &fakeOutbox{}
	h := newAddChannelHandler(channels, outbox, fixedNow)

	if _, err := h.Handle(context.Background(), testPrincipal, "wechat", "user@example.com"); !errors.Is(err, domain.ErrInvalidChannelKind) {
		t.Fatalf("invalid kind error = %v", err)
	}
	if _, err := h.Handle(context.Background(), testPrincipal, "email", "not-an-email"); !errors.Is(err, domain.ErrInvalidEmail) {
		t.Fatalf("invalid email error = %v", err)
	}
	if _, err := h.Handle(context.Background(), testPrincipal, "sms", "123"); !errors.Is(err, domain.ErrInvalidPhone) {
		t.Fatalf("invalid sms error = %v", err)
	}
}

func TestAddChannelRejectsDuplicate(t *testing.T) {
	channels := newFakeChannelStore()
	outbox := &fakeOutbox{}
	h := newAddChannelHandler(channels, outbox, fixedNow)

	if _, err := h.Handle(context.Background(), testPrincipal, "email", "user@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Handle(context.Background(), testPrincipal, "email", "user@example.com"); !errors.Is(err, domain.ErrChannelExists) {
		t.Fatalf("duplicate error = %v, want ErrChannelExists", err)
	}
}

func TestAddChannelOutboxFailurePersistsNothing(t *testing.T) {
	channels := newFakeChannelStore()
	outbox := &fakeOutbox{writeErr: domain.ErrSmsUnavailable}
	h := newAddChannelHandler(channels, outbox, fixedNow)

	_, err := h.Handle(context.Background(), testPrincipal, "sms", "+8613800137001")
	if !errors.Is(err, domain.ErrSmsUnavailable) {
		t.Fatalf("Handle() error = %v, want the outbox failure", err)
	}
	if list, _ := channels.ListByUser(context.Background(), testPrincipal.WorkspaceID, testPrincipal.UserID); len(list) != 0 {
		t.Fatalf("persisted %d channels after a failed send, want none (no orphan row)", len(list))
	}
	if len(outbox.messages) != 0 {
		t.Fatalf("outbox recorded %d messages on failure, want none", len(outbox.messages))
	}
}

func TestAddChannelDuplicateRejectsBeforeSend(t *testing.T) {
	channels := newFakeChannelStore()
	outbox := &fakeOutbox{}
	h := newAddChannelHandler(channels, outbox, fixedNow)

	if _, err := h.Handle(context.Background(), testPrincipal, "email", "user@example.com"); err != nil {
		t.Fatal(err)
	}
	sent := len(outbox.messages)
	if _, err := h.Handle(context.Background(), testPrincipal, "email", "user@example.com"); !errors.Is(err, domain.ErrChannelExists) {
		t.Fatalf("duplicate error = %v, want ErrChannelExists", err)
	}
	if len(outbox.messages) != sent {
		t.Fatalf("duplicate request sent another code: %d messages, want %d", len(outbox.messages), sent)
	}
}

func TestVerifyChannelMarksVerified(t *testing.T) {
	channels := newFakeChannelStore()
	outbox := &fakeOutbox{}
	add := newAddChannelHandler(channels, outbox, fixedNow)
	view, err := add.Handle(context.Background(), testPrincipal, "email", "user@example.com")
	if err != nil {
		t.Fatal(err)
	}

	verify := &VerifyChannelHandler{Channels: channels, Now: fixedNow}
	if err := verify.Handle(context.Background(), testPrincipal, view.ID, "222333"); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	channel, err := channels.ByID(context.Background(), testPrincipal.WorkspaceID, testPrincipal.UserID, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !channel.Verified {
		t.Fatal("channel not verified")
	}
}

func TestVerifyChannelWrongCodeAndNotFound(t *testing.T) {
	channels := newFakeChannelStore()
	outbox := &fakeOutbox{}
	add := newAddChannelHandler(channels, outbox, fixedNow)
	view, err := add.Handle(context.Background(), testPrincipal, "email", "user@example.com")
	if err != nil {
		t.Fatal(err)
	}

	verify := &VerifyChannelHandler{Channels: channels, Now: fixedNow}
	if err := verify.Handle(context.Background(), testPrincipal, view.ID, "000000"); !errors.Is(err, domain.ErrInvalidCode) {
		t.Fatalf("wrong code error = %v, want ErrInvalidCode", err)
	}
	if err := verify.Handle(context.Background(), testPrincipal, "missing-id", "222333"); !errors.Is(err, domain.ErrChannelNotFound) {
		t.Fatalf("not found error = %v, want ErrChannelNotFound", err)
	}
}

func TestSetChannelEnabled(t *testing.T) {
	channels := newFakeChannelStore()
	outbox := &fakeOutbox{}
	add := newAddChannelHandler(channels, outbox, fixedNow)
	view, err := add.Handle(context.Background(), testPrincipal, "sms", "+8613800137001")
	if err != nil {
		t.Fatal(err)
	}

	set := &SetChannelEnabledHandler{Channels: channels}
	updated, err := set.Handle(context.Background(), testPrincipal, view.ID, false)
	if err != nil {
		t.Fatalf("SetEnabled(false) error = %v", err)
	}
	if updated.Enabled {
		t.Fatal("channel still enabled after disable")
	}
	if _, err := set.Handle(context.Background(), testPrincipal, "missing-id", true); !errors.Is(err, domain.ErrChannelNotFound) {
		t.Fatalf("not found error = %v, want ErrChannelNotFound", err)
	}
}

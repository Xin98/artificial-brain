package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

type resendUoW struct{}

func (resendUoW) Run(ctx context.Context, work func(context.Context) error) error { return work(ctx) }

func TestResendChannelRotatesCodeAndRetainsIdentity(t *testing.T) {
	channels, outbox := newFakeChannelStore(), &fakeOutbox{}
	add := newAddChannelHandler(channels, outbox, fixedNow)
	view, err := add.Handle(context.Background(), testPrincipal, "email", "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	now := testNow.Add(time.Minute)
	h := &ResendChannelHandler{Channels: channels, Outbox: outbox, NewCode: func() (string, error) { return "444555", nil }, Now: func() time.Time { return now }, CodeTTL: 10 * time.Minute, UoW: resendUoW{}}
	if err := h.Handle(context.Background(), testPrincipal, view.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := channels.ByID(context.Background(), testPrincipal.WorkspaceID, testPrincipal.UserID, view.ID)
	if got.CodeHash != domain.HashCode("444555") || !got.CodeExpiresAt.Equal(now.Add(10*time.Minute)) || got.Verified || !got.Enabled || got.ID != view.ID || !got.CreatedAt.Equal(testNow) {
		t.Fatalf("rotated channel = %#v", got)
	}
	if len(outbox.messages) != 2 || outbox.messages[1].Purpose != "channel_verification" || outbox.messages[1].Code != "444555" {
		t.Fatalf("outbox = %#v", outbox.messages)
	}
	verify := &VerifyChannelHandler{Channels: channels, Now: func() time.Time { return now }}
	if err := verify.Handle(context.Background(), testPrincipal, view.ID, "222333"); !errors.Is(err, domain.ErrInvalidCode) {
		t.Fatalf("old code accepted: %v", err)
	}
	if err := verify.Handle(context.Background(), testPrincipal, view.ID, "444555"); err != nil {
		t.Fatalf("fresh code failed: %v", err)
	}
}

func TestResendChannelRejectsScopeVerifiedAndCooldownBeforeSend(t *testing.T) {
	for _, scenario := range []string{"foreign user", "foreign workspace", "verified", "cooldown"} {
		t.Run(scenario, func(t *testing.T) {
			channels, outbox := newFakeChannelStore(), &fakeOutbox{}
			view, err := newAddChannelHandler(channels, outbox, fixedNow).Handle(context.Background(), testPrincipal, "sms", "+8613800137001")
			if err != nil {
				t.Fatal(err)
			}
			p, now, want := testPrincipal, testNow.Add(time.Minute), domain.ErrChannelNotFound
			switch scenario {
			case "foreign user":
				p.UserID = "other"
			case "foreign workspace":
				p.WorkspaceID = "other"
			case "verified":
				c := channels.channels[view.ID]
				c.Verified = true
				channels.channels[view.ID] = c
				want = domain.ErrChannelAlreadyVerified
			case "cooldown":
				now = testNow.Add(59 * time.Second)
				want = domain.ErrRateLimited
			}
			h := &ResendChannelHandler{Channels: channels, Outbox: outbox, NewCode: func() (string, error) { t.Fatal("generated code for rejected request"); return "", nil }, Now: func() time.Time { return now }, CodeTTL: 10 * time.Minute, UoW: resendUoW{}}
			if err := h.Handle(context.Background(), p, view.ID); !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
			if len(outbox.messages) != 1 {
				t.Fatal("rejected request sent code")
			}
		})
	}
}

func TestResendChannelSendFailureRetainsPreviousCode(t *testing.T) {
	channels, outbox := newFakeChannelStore(), &fakeOutbox{}
	view, err := newAddChannelHandler(channels, outbox, fixedNow).Handle(context.Background(), testPrincipal, "email", "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	before := channels.channels[view.ID]
	outbox.writeErr = domain.ErrCodeDeliveryFailed
	h := &ResendChannelHandler{Channels: channels, Outbox: outbox, NewCode: func() (string, error) { return "444555", nil }, Now: func() time.Time { return testNow.Add(time.Minute) }, CodeTTL: 10 * time.Minute, UoW: resendUoW{}}
	if err := h.Handle(context.Background(), testPrincipal, view.ID); !errors.Is(err, domain.ErrCodeDeliveryFailed) {
		t.Fatalf("error = %v", err)
	}
	after := channels.channels[view.ID]
	if before.CodeHash != after.CodeHash || !before.CodeExpiresAt.Equal(*after.CodeExpiresAt) {
		t.Fatal("send failure invalidated previous code")
	}
}

package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

var testNow = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

func newRequestHandler(challenges *fakeChallengeStore, outbox *fakeOutbox, now func() time.Time) *RequestLoginChallengeHandler {
	gen := &idGenerator{}
	return &RequestLoginChallengeHandler{
		Challenges:   challenges,
		Outbox:       outbox,
		NewCode:      func() (string, error) { return "123456", nil },
		NewID:        gen.next,
		Now:          now,
		ChallengeTTL: 5 * time.Minute,
	}
}

func newVerifyHandler(challenges *fakeChallengeStore, users *fakeUserStore, workspaces *fakeWorkspaceStore, sessions *fakeSessionStore, now func() time.Time) *VerifyLoginChallengeHandler {
	gen := &idGenerator{}
	return &VerifyLoginChallengeHandler{
		Challenges: challenges,
		Users:      users,
		Workspaces: workspaces,
		Sessions:   sessions,
		NewID:      gen.next,
		NewToken:   func() (string, error) { return "token-abc", nil },
		Now:        now,
		SessionTTL: 168 * time.Hour,
	}
}

func fixedNow() time.Time { return testNow }

func TestRequestLoginChallengeWritesOutbox(t *testing.T) {
	challenges := newFakeChallengeStore()
	outbox := &fakeOutbox{}
	h := newRequestHandler(challenges, outbox, fixedNow)

	if err := h.Handle(context.Background(), "+8613800137000"); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(outbox.messages) != 1 {
		t.Fatalf("outbox messages = %d, want 1", len(outbox.messages))
	}
	msg := outbox.messages[0]
	if msg.Code != "123456" || msg.Purpose != "login" || msg.Channel != "sms" || msg.Address != "+8613800137000" {
		t.Fatalf("outbox message = %#v", msg)
	}
	count, err := challenges.CountByPhoneSince(context.Background(), "+8613800137000", testNow.Add(-time.Hour))
	if err != nil || count != 1 {
		t.Fatalf("challenge count = %d, err = %v, want 1", count, err)
	}
}

func TestRequestLoginChallengeRejectsInvalidPhone(t *testing.T) {
	challenges := newFakeChallengeStore()
	outbox := &fakeOutbox{}
	h := newRequestHandler(challenges, outbox, fixedNow)

	if err := h.Handle(context.Background(), "not-a-phone"); !errors.Is(err, domain.ErrInvalidPhone) {
		t.Fatalf("Handle() error = %v, want ErrInvalidPhone", err)
	}
	if len(outbox.messages) != 0 {
		t.Fatalf("outbox messages = %d, want 0", len(outbox.messages))
	}
}

func TestRequestLoginChallengeRateLimits(t *testing.T) {
	challenges := newFakeChallengeStore()
	outbox := &fakeOutbox{}
	h := newRequestHandler(challenges, outbox, fixedNow)

	for i := 0; i < MaxChallengesPerPhonePerHour; i++ {
		if err := h.Handle(context.Background(), "+8613800137000"); err != nil {
			t.Fatalf("request %d error = %v", i, err)
		}
	}
	if err := h.Handle(context.Background(), "+8613800137000"); !errors.Is(err, domain.ErrRateLimited) {
		t.Fatalf("6th request error = %v, want ErrRateLimited", err)
	}
}

func TestVerifyLoginChallengeRegistersUserAndWorkspace(t *testing.T) {
	challenges := newFakeChallengeStore()
	users := newFakeUserStore()
	workspaces := newFakeWorkspaceStore()
	sessions := newFakeSessionStore()
	outbox := &fakeOutbox{}
	phone := "+8613800137000"

	if err := newRequestHandler(challenges, outbox, fixedNow).Handle(context.Background(), phone); err != nil {
		t.Fatalf("request error = %v", err)
	}
	result, err := newVerifyHandler(challenges, users, workspaces, sessions, fixedNow).Handle(context.Background(), phone, "123456")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Token != "token-abc" {
		t.Fatalf("token = %q", result.Token)
	}
	if result.Principal.UserID == "" || result.Principal.WorkspaceID == "" || result.Principal.SessionID == "" {
		t.Fatalf("principal = %#v", result.Principal)
	}
	if _, err := users.ByPhone(context.Background(), phone); err != nil {
		t.Fatalf("user not registered: %v", err)
	}
	if len(workspaces.workspaces) != 1 {
		t.Fatalf("workspaces = %d, want 1", len(workspaces.workspaces))
	}
	if _, err := sessions.ByTokenHash(context.Background(), domain.HashCode("token-abc")); err != nil {
		t.Fatalf("session not stored by token hash: %v", err)
	}
}

func TestVerifyLoginChallengeReusesExistingUser(t *testing.T) {
	challenges := newFakeChallengeStore()
	users := newFakeUserStore()
	workspaces := newFakeWorkspaceStore()
	sessions := newFakeSessionStore()
	outbox := &fakeOutbox{}
	phone := "+8613800137000"
	verify := newVerifyHandler(challenges, users, workspaces, sessions, fixedNow)
	request := newRequestHandler(challenges, outbox, fixedNow)

	if err := request.Handle(context.Background(), phone); err != nil {
		t.Fatal(err)
	}
	first, err := verify.Handle(context.Background(), phone, "123456")
	if err != nil {
		t.Fatal(err)
	}
	if err := request.Handle(context.Background(), phone); err != nil {
		t.Fatal(err)
	}
	second, err := verify.Handle(context.Background(), phone, "123456")
	if err != nil {
		t.Fatal(err)
	}
	if first.Principal.UserID != second.Principal.UserID || first.Principal.WorkspaceID != second.Principal.WorkspaceID {
		t.Fatalf("second login created a new identity: %v vs %v", first.Principal, second.Principal)
	}
	if len(workspaces.workspaces) != 1 || len(users.users) != 1 {
		t.Fatalf("expected a single user/workspace, got %d users %d workspaces", len(users.users), len(workspaces.workspaces))
	}
}

func TestVerifyLoginChallengeWrongCodeIncrementsAttempts(t *testing.T) {
	challenges := newFakeChallengeStore()
	users := newFakeUserStore()
	workspaces := newFakeWorkspaceStore()
	sessions := newFakeSessionStore()
	outbox := &fakeOutbox{}
	phone := "+8613800137000"

	if err := newRequestHandler(challenges, outbox, fixedNow).Handle(context.Background(), phone); err != nil {
		t.Fatal(err)
	}
	verify := newVerifyHandler(challenges, users, workspaces, sessions, fixedNow)

	for i := 0; i < domain.MaxVerifyAttempts; i++ {
		if _, err := verify.Handle(context.Background(), phone, "000000"); err == nil {
			t.Fatalf("wrong code %d accepted", i)
		}
	}
	if _, err := verify.Handle(context.Background(), phone, "123456"); !errors.Is(err, domain.ErrTooManyAttempts) {
		t.Fatalf("after exhaustion error = %v, want ErrTooManyAttempts", err)
	}
}

func TestVerifyLoginChallengeExpired(t *testing.T) {
	challenges := newFakeChallengeStore()
	users := newFakeUserStore()
	workspaces := newFakeWorkspaceStore()
	sessions := newFakeSessionStore()
	outbox := &fakeOutbox{}
	phone := "+8613800137000"

	if err := newRequestHandler(challenges, outbox, fixedNow).Handle(context.Background(), phone); err != nil {
		t.Fatal(err)
	}
	later := func() time.Time { return testNow.Add(6 * time.Minute) }
	if _, err := newVerifyHandler(challenges, users, workspaces, sessions, later).Handle(context.Background(), phone, "123456"); !errors.Is(err, domain.ErrChallengeExpired) {
		t.Fatalf("error = %v, want ErrChallengeExpired", err)
	}
}

func TestVerifyLoginChallengeSingleUse(t *testing.T) {
	challenges := newFakeChallengeStore()
	users := newFakeUserStore()
	workspaces := newFakeWorkspaceStore()
	sessions := newFakeSessionStore()
	outbox := &fakeOutbox{}
	phone := "+8613800137000"

	if err := newRequestHandler(challenges, outbox, fixedNow).Handle(context.Background(), phone); err != nil {
		t.Fatal(err)
	}
	verify := newVerifyHandler(challenges, users, workspaces, sessions, fixedNow)
	if _, err := verify.Handle(context.Background(), phone, "123456"); err != nil {
		t.Fatal(err)
	}
	if _, err := verify.Handle(context.Background(), phone, "123456"); !errors.Is(err, domain.ErrChallengeConsumed) {
		t.Fatalf("reuse error = %v, want ErrChallengeConsumed", err)
	}
}

func TestVerifyLoginChallengeStoresHashNotPlaintext(t *testing.T) {
	challenges := newFakeChallengeStore()
	outbox := &fakeOutbox{}
	phone := "+8613800137000"

	if err := newRequestHandler(challenges, outbox, fixedNow).Handle(context.Background(), phone); err != nil {
		t.Fatal(err)
	}
	challenge, err := challenges.ActiveByPhone(context.Background(), phone)
	if err != nil {
		t.Fatal(err)
	}
	if challenge.CodeHash == "123456" {
		t.Fatal("challenge stored plaintext code")
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	challenges := newFakeChallengeStore()
	users := newFakeUserStore()
	workspaces := newFakeWorkspaceStore()
	sessions := newFakeSessionStore()
	outbox := &fakeOutbox{}
	phone := "+8613800137000"

	if err := newRequestHandler(challenges, outbox, fixedNow).Handle(context.Background(), phone); err != nil {
		t.Fatal(err)
	}
	result, err := newVerifyHandler(challenges, users, workspaces, sessions, fixedNow).Handle(context.Background(), phone, "123456")
	if err != nil {
		t.Fatal(err)
	}

	logout := &LogoutHandler{Sessions: sessions, Now: fixedNow}
	if err := logout.Handle(context.Background(), result.Principal.SessionID); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	session, err := sessions.ByID(context.Background(), result.Principal.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !session.IsRevoked() {
		t.Fatal("session not revoked after logout")
	}
}

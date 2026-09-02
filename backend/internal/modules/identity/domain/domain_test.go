package domain

import (
	"errors"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

func TestNewPhone(t *testing.T) {
	for _, valid := range []string{"+8613800137000", "13800137000", "+14155552671"} {
		if _, err := NewPhone(valid); err != nil {
			t.Fatalf("NewPhone(%q) error = %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "123", "abcdefghijk", "+0123456789", "12345"} {
		if _, err := NewPhone(invalid); !errors.Is(err, ErrInvalidPhone) {
			t.Fatalf("NewPhone(%q) error = %v, want ErrInvalidPhone", invalid, err)
		}
	}
}

func TestNewEmail(t *testing.T) {
	if _, err := NewEmail("user@example.com"); err != nil {
		t.Fatalf("NewEmail(valid) error = %v", err)
	}
	for _, invalid := range []string{"", "no-at-sign", "a@b", "a b@c.com"} {
		if _, err := NewEmail(invalid); !errors.Is(err, ErrInvalidEmail) {
			t.Fatalf("NewEmail(%q) error = %v, want ErrInvalidEmail", invalid, err)
		}
	}
}

func TestNewEmailCaseNormalized(t *testing.T) {
	got, err := NewEmail("User@Example.COM")
	if err != nil {
		t.Fatalf("NewEmail() error = %v", err)
	}
	if got.String() != "user@example.com" {
		t.Fatalf("NewEmail(mixed case) = %q, want lowercase canonical form", got.String())
	}
}

func TestCodeValidationAndHash(t *testing.T) {
	if _, err := NewCode("123456"); err != nil {
		t.Fatalf("NewCode(valid) error = %v", err)
	}
	for _, invalid := range []string{"", "12345", "1234567", "abcdef"} {
		if _, err := NewCode(invalid); !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("NewCode(%q) error = %v, want ErrInvalidCode", invalid, err)
		}
	}
	if HashCode("123456") != HashCode("123456") {
		t.Fatal("HashCode is not deterministic")
	}
	if HashCode("123456") == HashCode("654321") {
		t.Fatal("HashCode collided for different codes")
	}
}

func TestChallengeLifecycle(t *testing.T) {
	challenge := &LoginChallenge{
		ID:        "c1",
		Phone:     "+8613800137000",
		CodeHash:  HashCode("123456"),
		CreatedAt: fixedNow,
		ExpiresAt: fixedNow.Add(5 * time.Minute),
	}

	if challenge.IsExpired(fixedNow) {
		t.Fatal("challenge expired before its expiry")
	}
	if !challenge.IsExpired(fixedNow.Add(5 * time.Minute)) {
		t.Fatal("challenge not expired at its expiry")
	}
	if !challenge.Matches(HashCode("123456")) {
		t.Fatal("challenge did not match its own code hash")
	}
	if challenge.Matches(HashCode("000000")) {
		t.Fatal("challenge matched a wrong code hash")
	}

	if err := challenge.Consume(fixedNow); err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if !challenge.IsConsumed() {
		t.Fatal("challenge not consumed after Consume")
	}
	if err := challenge.Consume(fixedNow); !errors.Is(err, ErrChallengeConsumed) {
		t.Fatalf("second Consume() error = %v, want ErrChallengeConsumed", err)
	}
}

func TestChallengeFailedAttemptsExhaust(t *testing.T) {
	challenge := &LoginChallenge{ExpiresAt: fixedNow.Add(time.Minute)}
	for i := 0; i < MaxVerifyAttempts-1; i++ {
		if challenge.RegisterFailedAttempt() {
			t.Fatalf("challenge exhausted after %d attempts", i+1)
		}
	}
	if !challenge.RegisterFailedAttempt() {
		t.Fatalf("challenge not exhausted after %d attempts", MaxVerifyAttempts)
	}
}

func TestSessionActiveAndRevoke(t *testing.T) {
	session := &Session{
		ID:          "s1",
		UserID:      "u1",
		WorkspaceID: "w1",
		TokenHash:   "hash",
		CreatedAt:   fixedNow,
		ExpiresAt:   fixedNow.Add(24 * time.Hour),
	}
	if !session.IsActive(fixedNow) {
		t.Fatal("fresh session not active")
	}
	if session.IsActive(fixedNow.Add(24 * time.Hour)) {
		t.Fatal("session active at its expiry")
	}
	session.Revoke(fixedNow)
	if !session.IsRevoked() || session.IsActive(fixedNow) {
		t.Fatal("revoked session still active")
	}
}

func TestChannelVerifyAndEnable(t *testing.T) {
	expires := fixedNow.Add(10 * time.Minute)
	channel := &ContactChannel{
		ID:            "ch1",
		UserID:        "u1",
		WorkspaceID:   "w1",
		Kind:          ChannelKindEmail,
		Address:       "user@example.com",
		Enabled:       true,
		CodeHash:      HashCode("222333"),
		CodeExpiresAt: &expires,
	}

	if channel.Usable() {
		t.Fatal("unverified channel usable")
	}
	if err := channel.Verify(HashCode("000000"), fixedNow); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("Verify(wrong) error = %v, want ErrInvalidCode", err)
	}
	if err := channel.Verify(HashCode("222333"), fixedNow); err != nil {
		t.Fatalf("Verify(correct) error = %v", err)
	}
	if !channel.Verified || !channel.Usable() {
		t.Fatal("channel not usable after verification")
	}

	channel.SetEnabled(false)
	if channel.Usable() {
		t.Fatal("disabled channel usable")
	}
}

func TestChannelVerifyExpiredCode(t *testing.T) {
	expired := fixedNow.Add(-time.Minute)
	channel := &ContactChannel{CodeHash: HashCode("111111"), CodeExpiresAt: &expired}
	if err := channel.Verify(HashCode("111111"), fixedNow); !errors.Is(err, ErrChannelCodeExpired) {
		t.Fatalf("Verify(expired) error = %v, want ErrChannelCodeExpired", err)
	}
}

func TestNewChannelKind(t *testing.T) {
	for _, valid := range []string{"email", "sms"} {
		if _, err := NewChannelKind(valid); err != nil {
			t.Fatalf("NewChannelKind(%q) error = %v", valid, err)
		}
	}
	if _, err := NewChannelKind("wechat"); !errors.Is(err, ErrInvalidChannelKind) {
		t.Fatalf("NewChannelKind(wechat) error = %v, want ErrInvalidChannelKind", err)
	}
}

package domain

import (
	"errors"
	"testing"
	"time"
)

var testNow = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

func TestNewReminderPlanDefaultsToPlanned(t *testing.T) {
	plan, err := NewReminderPlan("plan-1", "ws-1", "todo-1", 1, testNow.Add(time.Hour), []string{"sms"}, testNow)
	if err != nil {
		t.Fatalf("NewReminderPlan() error = %v", err)
	}
	if plan.ID != "plan-1" || plan.WorkspaceID != "ws-1" || plan.TodoID != "todo-1" {
		t.Fatalf("plan identity = %#v", plan)
	}
	if plan.TodoReminderVersion != 1 {
		t.Fatalf("plan.TodoReminderVersion = %d, want 1", plan.TodoReminderVersion)
	}
	if !plan.ScheduledAtUTC.Equal(testNow.Add(time.Hour)) {
		t.Fatalf("plan.ScheduledAtUTC = %v", plan.ScheduledAtUTC)
	}
	if len(plan.RequestedChannels) != 1 || plan.RequestedChannels[0] != "sms" {
		t.Fatalf("plan.RequestedChannels = %#v", plan.RequestedChannels)
	}
	if plan.Status != StatusPlanned {
		t.Fatalf("plan.Status = %q, want %q", plan.Status, StatusPlanned)
	}
	if !plan.CreatedAt.Equal(testNow) {
		t.Fatalf("plan.CreatedAt = %v", plan.CreatedAt)
	}
	if plan.RevokedAt != nil {
		t.Fatalf("plan.RevokedAt = %v, want nil", plan.RevokedAt)
	}
}

func TestNewReminderPlanRequiresScheduledAt(t *testing.T) {
	_, err := NewReminderPlan("plan-1", "ws-1", "todo-1", 1, time.Time{}, []string{"sms"}, testNow)
	if !errors.Is(err, ErrMissingSchedule) {
		t.Fatalf("NewReminderPlan(zero schedule) error = %v, want ErrMissingSchedule", err)
	}
}

func TestNewReminderPlanNormalizesNilChannelsToEmpty(t *testing.T) {
	plan, err := NewReminderPlan("plan-1", "ws-1", "todo-1", 1, testNow, nil, testNow)
	if err != nil {
		t.Fatalf("NewReminderPlan() error = %v", err)
	}
	if plan.RequestedChannels == nil {
		t.Fatal("plan.RequestedChannels is nil, want empty non-nil slice")
	}
	if len(plan.RequestedChannels) != 0 {
		t.Fatalf("plan.RequestedChannels = %#v, want empty", plan.RequestedChannels)
	}
}

func TestReminderPlanRevokeTransitionsPlannedToRevoked(t *testing.T) {
	plan, err := NewReminderPlan("plan-1", "ws-1", "todo-1", 1, testNow, nil, testNow)
	if err != nil {
		t.Fatalf("NewReminderPlan() error = %v", err)
	}
	if plan.IsRevoked() {
		t.Fatal("new plan reports revoked")
	}
	if err := plan.Revoke(testNow.Add(time.Minute)); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if !plan.IsRevoked() || plan.Status != StatusRevoked {
		t.Fatalf("plan after Revoke = %#v", plan)
	}
	if plan.RevokedAt == nil || !plan.RevokedAt.Equal(testNow.Add(time.Minute)) {
		t.Fatalf("plan.RevokedAt = %v", plan.RevokedAt)
	}
}

func TestReminderPlanRevokeIsSingleUse(t *testing.T) {
	plan, err := NewReminderPlan("plan-1", "ws-1", "todo-1", 1, testNow, nil, testNow)
	if err != nil {
		t.Fatalf("NewReminderPlan() error = %v", err)
	}
	if err := plan.Revoke(testNow); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if err := plan.Revoke(testNow.Add(time.Minute)); !errors.Is(err, ErrPlanAlreadyRevoked) {
		t.Fatalf("second Revoke() error = %v, want ErrPlanAlreadyRevoked", err)
	}
	if !plan.RevokedAt.Equal(testNow) {
		t.Fatalf("second Revoke changed RevokedAt to %v", plan.RevokedAt)
	}
}

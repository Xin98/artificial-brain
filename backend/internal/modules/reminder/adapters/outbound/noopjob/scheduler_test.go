package noopjob

import (
	"context"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
)

func TestNewSatisfiesJobSchedulerPort(t *testing.T) {
	var _ ports.JobScheduler = New()
}

func TestScheduleAlwaysReturnsEmptySlice(t *testing.T) {
	scheduler := New()
	job := ports.ReminderJob{
		PlanID:              "plan-1",
		WorkspaceID:         "ws-1",
		OwnerUserID:         "user-1",
		TodoID:              "todo-1",
		TodoReminderVersion: 1,
		ScheduledAtUTC:      time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC),
		Channels:            []string{"sms"},
	}
	scheduled, err := scheduler.Schedule(context.Background(), job)
	if err != nil {
		t.Fatalf("Schedule() error = %v, want nil", err)
	}
	if scheduled == nil || len(scheduled) != 0 {
		t.Fatalf("Schedule() = %#v, want empty slice", scheduled)
	}
}

func TestCancelAlwaysReturnsNil(t *testing.T) {
	scheduler := New()
	if err := scheduler.Cancel(context.Background(), 42); err != nil {
		t.Fatalf("Cancel() error = %v, want nil", err)
	}
}

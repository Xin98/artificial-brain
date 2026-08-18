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

func TestScheduleAlwaysReturnsNil(t *testing.T) {
	scheduler := New()
	job := ports.ReminderJob{
		PlanID:              "plan-1",
		WorkspaceID:         "ws-1",
		TodoID:              "todo-1",
		TodoReminderVersion: 1,
		ScheduledAtUTC:      time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC),
		Channels:            []string{"sms"},
	}
	if err := scheduler.Schedule(context.Background(), job); err != nil {
		t.Fatalf("Schedule() error = %v, want nil", err)
	}
}

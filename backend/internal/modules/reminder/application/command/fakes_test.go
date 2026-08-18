package command

import (
	"context"
	"errors"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

type fakePlanStore struct {
	saved       []domain.ReminderPlan
	saveErr     error
	revokeCalls []revokeCall
	revokeErr   error
}

type revokeCall struct {
	workspaceID         string
	todoID              string
	upToReminderVersion int
	now                 time.Time
}

func newFakePlanStore() *fakePlanStore { return &fakePlanStore{} }

func (s *fakePlanStore) Save(_ context.Context, plan domain.ReminderPlan) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = append(s.saved, plan)
	return nil
}

func (s *fakePlanStore) RevokePlanned(_ context.Context, workspaceID, todoID string, upToReminderVersion int, now time.Time) error {
	s.revokeCalls = append(s.revokeCalls, revokeCall{workspaceID, todoID, upToReminderVersion, now})
	return s.revokeErr
}

type fakeScheduler struct {
	jobs        []ports.ReminderJob
	scheduleErr error
}

func newFakeScheduler() *fakeScheduler { return &fakeScheduler{} }

func (s *fakeScheduler) Schedule(_ context.Context, job ports.ReminderJob) error {
	if s.scheduleErr != nil {
		return s.scheduleErr
	}
	s.jobs = append(s.jobs, job)
	return nil
}

var errSaveFailed = errors.New("save failed")
var errScheduleFailed = errors.New("schedule failed")

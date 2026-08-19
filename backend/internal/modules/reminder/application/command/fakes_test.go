package command

import (
	"context"
	"errors"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
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

func (s *fakePlanStore) Get(context.Context, string, string) (domain.ReminderPlan, error) {
	return domain.ReminderPlan{}, domain.ErrPlanNotFound
}

func (s *fakePlanStore) RevokePlanned(_ context.Context, workspaceID, todoID string, upToReminderVersion int, now time.Time) error {
	s.revokeCalls = append(s.revokeCalls, revokeCall{workspaceID, todoID, upToReminderVersion, now})
	return s.revokeErr
}

type fakeScheduler struct {
	jobs        []ports.ReminderJob
	scheduleErr error
	scheduled   []ports.ScheduledChannel
	cancelCalls []int64
	cancelErr   error
}

func newFakeScheduler() *fakeScheduler { return &fakeScheduler{} }

func (s *fakeScheduler) Schedule(_ context.Context, job ports.ReminderJob) ([]ports.ScheduledChannel, error) {
	if s.scheduleErr != nil {
		return nil, s.scheduleErr
	}
	s.jobs = append(s.jobs, job)
	return s.scheduled, nil
}

func (s *fakeScheduler) Cancel(_ context.Context, jobID int64) error {
	s.cancelCalls = append(s.cancelCalls, jobID)
	return s.cancelErr
}

type fakeDeliveryStore struct {
	saved                 []domain.ReminderDelivery
	saveErr               error
	setProviderJobIDCalls []providerJobIDCall
	setProviderJobIDErr   error
	plannedJobIDs         []int64
	plannedJobIDsErr      error
}

type providerJobIDCall struct {
	workspaceID string
	deliveryID  string
	jobID       int64
}

func newFakeDeliveryStore() *fakeDeliveryStore { return &fakeDeliveryStore{} }

func (s *fakeDeliveryStore) Save(_ context.Context, delivery domain.ReminderDelivery) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = append(s.saved, delivery)
	return nil
}

func (s *fakeDeliveryStore) Update(context.Context, domain.ReminderDelivery) error { return nil }

func (s *fakeDeliveryStore) ByIdempotencyKey(context.Context, string, string) (domain.ReminderDelivery, error) {
	return domain.ReminderDelivery{}, domain.ErrDeliveryNotFound
}

func (s *fakeDeliveryStore) ByProviderMessageID(context.Context, string) (domain.ReminderDelivery, error) {
	return domain.ReminderDelivery{}, domain.ErrDeliveryNotFound
}

func (s *fakeDeliveryStore) SetProviderJobID(_ context.Context, workspaceID, deliveryID string, jobID int64) error {
	if s.setProviderJobIDErr != nil {
		return s.setProviderJobIDErr
	}
	s.setProviderJobIDCalls = append(s.setProviderJobIDCalls, providerJobIDCall{workspaceID, deliveryID, jobID})
	return nil
}

func (s *fakeDeliveryStore) PlannedJobIDs(context.Context, string, string, int) ([]int64, error) {
	return s.plannedJobIDs, s.plannedJobIDsErr
}

func (s *fakeDeliveryStore) Stats(context.Context, string) (dto.DeliveryCounts, error) {
	return dto.DeliveryCounts{}, nil
}

func (s *fakeDeliveryStore) List(context.Context, string, dto.DeliveryFilter) ([]domain.ReminderDelivery, error) {
	return nil, nil
}

var errSaveFailed = errors.New("save failed")
var errScheduleFailed = errors.New("schedule failed")
var errCancelFailed = errors.New("cancel failed")

package command

import (
	"context"
	"errors"
	"sort"
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
	plan        domain.ReminderPlan
	hasPlan     bool
	getErr      error
	getCalls    []planGetCall
}

type revokeCall struct {
	workspaceID         string
	todoID              string
	upToReminderVersion int
	now                 time.Time
}

type planGetCall struct {
	workspaceID string
	planID      string
}

func newFakePlanStore() *fakePlanStore { return &fakePlanStore{} }

func (s *fakePlanStore) Save(_ context.Context, plan domain.ReminderPlan) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = append(s.saved, plan)
	return nil
}

// Get returns the configured plan; without one it reports the plan missing,
// mirroring the scoped postgres store.
func (s *fakePlanStore) Get(_ context.Context, workspaceID, planID string) (domain.ReminderPlan, error) {
	s.getCalls = append(s.getCalls, planGetCall{workspaceID, planID})
	if s.getErr != nil {
		return domain.ReminderPlan{}, s.getErr
	}
	if s.hasPlan {
		return s.plan, nil
	}
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

// fakeDeliveryStore is a stateful in-memory delivery store: Save inserts and
// rejects duplicate idempotency keys, Update replaces, and the two readers
// resolve against the seeded and written rows. saved and updated keep the
// full call history for assertions.
type fakeDeliveryStore struct {
	saved                       []domain.ReminderDelivery
	saveErr                     error
	updated                     []domain.ReminderDelivery
	updateErr                   error
	keyErr                      error
	providerErr                 error
	rows                        map[string]domain.ReminderDelivery
	setProviderJobIDCalls       []providerJobIDCall
	setProviderJobIDErr         error
	plannedJobIDs               []int64
	plannedJobIDsErr            error
	scheduledForSuppressionErr  error
	scheduledForSuppressionArgs []suppressionScopeCall
}

type suppressionScopeCall struct {
	workspaceID         string
	todoID              string
	upToReminderVersion int
}

type providerJobIDCall struct {
	workspaceID string
	deliveryID  string
	jobID       int64
}

func newFakeDeliveryStore() *fakeDeliveryStore {
	return &fakeDeliveryStore{rows: make(map[string]domain.ReminderDelivery)}
}

// seed plants a delivery row as though the planner had created it.
func (s *fakeDeliveryStore) seed(delivery domain.ReminderDelivery) {
	s.rows[delivery.IdempotencyKey] = delivery
}

func (s *fakeDeliveryStore) Save(_ context.Context, delivery domain.ReminderDelivery) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	if _, exists := s.rows[delivery.IdempotencyKey]; exists {
		return domain.ErrDeliveryExists
	}
	s.rows[delivery.IdempotencyKey] = delivery
	s.saved = append(s.saved, delivery)
	return nil
}

func (s *fakeDeliveryStore) Update(_ context.Context, delivery domain.ReminderDelivery) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.rows[delivery.IdempotencyKey] = delivery
	s.updated = append(s.updated, delivery)
	return nil
}

func (s *fakeDeliveryStore) ByIdempotencyKey(_ context.Context, _, key string) (domain.ReminderDelivery, error) {
	if s.keyErr != nil {
		return domain.ReminderDelivery{}, s.keyErr
	}
	delivery, ok := s.rows[key]
	if !ok {
		return domain.ReminderDelivery{}, domain.ErrDeliveryNotFound
	}
	return delivery, nil
}

func (s *fakeDeliveryStore) ByProviderMessageID(_ context.Context, providerMessageID string) (domain.ReminderDelivery, error) {
	if s.providerErr != nil {
		return domain.ReminderDelivery{}, s.providerErr
	}
	for _, delivery := range s.rows {
		if delivery.ProviderMessageID != nil && *delivery.ProviderMessageID == providerMessageID {
			return delivery, nil
		}
	}
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

// ScheduledForSuppression mirrors the postgres adapter: it returns only the
// seeded rows that are workspace+todo scoped, at or below the reminder version
// cutoff, and still scheduled — sending and final rows are never returned.
func (s *fakeDeliveryStore) ScheduledForSuppression(_ context.Context, workspaceID, todoID string, upToReminderVersion int) ([]domain.ReminderDelivery, error) {
	s.scheduledForSuppressionArgs = append(s.scheduledForSuppressionArgs, suppressionScopeCall{workspaceID, todoID, upToReminderVersion})
	if s.scheduledForSuppressionErr != nil {
		return nil, s.scheduledForSuppressionErr
	}
	result := []domain.ReminderDelivery{}
	for _, delivery := range s.rows {
		if delivery.WorkspaceID != workspaceID || delivery.TodoID != todoID {
			continue
		}
		if delivery.TodoReminderVersion > upToReminderVersion {
			continue
		}
		if delivery.State != domain.StateScheduled {
			continue
		}
		result = append(result, delivery)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (s *fakeDeliveryStore) Stats(context.Context, string) (dto.DeliveryCounts, error) {
	return dto.DeliveryCounts{}, nil
}

func (s *fakeDeliveryStore) List(context.Context, string, dto.DeliveryFilter) ([]domain.ReminderDelivery, error) {
	return nil, nil
}

type fakeTodoReader struct {
	getCalls []todoGetCall
	view     dto.TodoView
	err      error
}

type todoGetCall struct {
	workspaceID string
	ownerUserID string
	todoID      string
}

func newFakeTodoReader() *fakeTodoReader { return &fakeTodoReader{} }

func (r *fakeTodoReader) Get(_ context.Context, workspaceID, ownerUserID, todoID string) (dto.TodoView, error) {
	r.getCalls = append(r.getCalls, todoGetCall{workspaceID, ownerUserID, todoID})
	return r.view, r.err
}

type fakeChannelResolver struct {
	resolveCalls []channelResolveCall
	endpoint     dto.ChannelEndpoint
	err          error
}

type channelResolveCall struct {
	workspaceID string
	userID      string
	channel     string
}

func newFakeChannelResolver() *fakeChannelResolver { return &fakeChannelResolver{} }

func (r *fakeChannelResolver) Resolve(_ context.Context, workspaceID, userID, channel string) (dto.ChannelEndpoint, error) {
	r.resolveCalls = append(r.resolveCalls, channelResolveCall{workspaceID, userID, channel})
	return r.endpoint, r.err
}

// fakeNotifier satisfies both ports.EmailNotifier and ports.SmsNotifier and
// records every message it is asked to send.
type fakeNotifier struct {
	calls  []dto.ReminderMessage
	result dto.SendResult
	err    error
}

func newFakeNotifier() *fakeNotifier { return &fakeNotifier{} }

func (n *fakeNotifier) Send(_ context.Context, message dto.ReminderMessage) (dto.SendResult, error) {
	n.calls = append(n.calls, message)
	return n.result, n.err
}

var errSaveFailed = errors.New("save failed")
var errScheduleFailed = errors.New("schedule failed")
var errCancelFailed = errors.New("cancel failed")
var errProviderTransient = errors.New("provider timeout")

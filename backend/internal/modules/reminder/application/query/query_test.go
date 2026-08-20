package query

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

var fixedNow = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

type fakeDeliveryStore struct {
	statsCalls []string
	stats      dto.DeliveryCounts
	statsErr   error
	listCalls  []listCall
	rows       []domain.ReminderDelivery
	listErr    error
}

type listCall struct {
	workspaceID string
	filter      dto.DeliveryFilter
}

func newFakeDeliveryStore() *fakeDeliveryStore { return &fakeDeliveryStore{} }

func (s *fakeDeliveryStore) Save(context.Context, domain.ReminderDelivery) error { return nil }

func (s *fakeDeliveryStore) Update(context.Context, domain.ReminderDelivery) error { return nil }

func (s *fakeDeliveryStore) ByIdempotencyKey(context.Context, string, string) (domain.ReminderDelivery, error) {
	return domain.ReminderDelivery{}, domain.ErrDeliveryNotFound
}

func (s *fakeDeliveryStore) ByProviderMessageID(context.Context, string) (domain.ReminderDelivery, error) {
	return domain.ReminderDelivery{}, domain.ErrDeliveryNotFound
}

func (s *fakeDeliveryStore) SetProviderJobID(context.Context, string, string, int64) error {
	return nil
}

func (s *fakeDeliveryStore) PlannedJobIDs(context.Context, string, string, int) ([]int64, error) {
	return nil, nil
}

func (s *fakeDeliveryStore) ScheduledForSuppression(context.Context, string, string, int) ([]domain.ReminderDelivery, error) {
	return nil, nil
}

func (s *fakeDeliveryStore) Stats(_ context.Context, workspaceID string) (dto.DeliveryCounts, error) {
	s.statsCalls = append(s.statsCalls, workspaceID)
	return s.stats, s.statsErr
}

func (s *fakeDeliveryStore) List(_ context.Context, workspaceID string, filter dto.DeliveryFilter) ([]domain.ReminderDelivery, error) {
	s.listCalls = append(s.listCalls, listCall{workspaceID, filter})
	return s.rows, s.listErr
}

type fakeOpsStore struct {
	calls []opsCall
	view  dto.OpsView
	err   error
}

type opsCall struct {
	now    time.Time
	window time.Duration
}

func newFakeOpsStore() *fakeOpsStore { return &fakeOpsStore{} }

func (s *fakeOpsStore) ReminderOps(_ context.Context, now time.Time, window time.Duration) (dto.OpsView, error) {
	s.calls = append(s.calls, opsCall{now, window})
	return s.view, s.err
}

var errStatsFailed = errors.New("stats failed")

func TestDeliveryStatsDelegatesToStore(t *testing.T) {
	store := newFakeDeliveryStore()
	store.stats = dto.DeliveryCounts{Scheduled: 1, Sending: 2, Retrying: 3, Succeeded: 4, Failed: 5, Suppressed: 6}
	handler := &DeliveryStatsHandler{Deliveries: store}

	counts, err := handler.Handle(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if counts != store.stats {
		t.Fatalf("counts = %#v, want %#v", counts, store.stats)
	}
	if len(store.statsCalls) != 1 || store.statsCalls[0] != "ws-1" {
		t.Fatalf("stats calls = %#v, want one ws-1 call", store.statsCalls)
	}
}

func TestDeliveryStatsPropagatesStoreError(t *testing.T) {
	store := newFakeDeliveryStore()
	store.statsErr = errStatsFailed
	handler := &DeliveryStatsHandler{Deliveries: store}

	if _, err := handler.Handle(context.Background(), "ws-1"); !errors.Is(err, errStatsFailed) {
		t.Fatalf("Handle() error = %v, want errStatsFailed", err)
	}
}

func TestListDeliveriesClampsFilterBeforeDelegating(t *testing.T) {
	tests := []struct {
		name string
		in   dto.DeliveryFilter
		want dto.DeliveryFilter
	}{
		{"defaults and negative offset", dto.DeliveryFilter{Status: "failed", Limit: 0, Offset: -5}, dto.DeliveryFilter{Status: "failed", Limit: 50, Offset: 0}},
		{"limit capped at max", dto.DeliveryFilter{Status: "sending", Limit: 300, Offset: 7}, dto.DeliveryFilter{Status: "sending", Limit: 200, Offset: 7}},
		{"values inside bounds pass through", dto.DeliveryFilter{Status: "succeeded", Limit: 25, Offset: 10}, dto.DeliveryFilter{Status: "succeeded", Limit: 25, Offset: 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeDeliveryStore()
			handler := &ListDeliveriesHandler{Deliveries: store}

			if _, err := handler.Handle(context.Background(), "ws-1", tt.in); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if len(store.listCalls) != 1 {
				t.Fatalf("list calls = %d, want 1", len(store.listCalls))
			}
			call := store.listCalls[0]
			if call.workspaceID != "ws-1" || call.filter != tt.want {
				t.Fatalf("list call = %#v, want workspace ws-1 filter %#v", call, tt.want)
			}
		})
	}
}

func TestListDeliveriesReturnsStoreRows(t *testing.T) {
	store := newFakeDeliveryStore()
	store.rows = []domain.ReminderDelivery{{ID: "delivery-1"}, {ID: "delivery-2"}}
	handler := &ListDeliveriesHandler{Deliveries: store}

	rows, err := handler.Handle(context.Background(), "ws-1", dto.DeliveryFilter{})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(rows) != 2 || rows[0].ID != "delivery-1" || rows[1].ID != "delivery-2" {
		t.Fatalf("rows = %#v, want the two store rows", rows)
	}
}

func TestReminderOpsPassesClockAndDayWindow(t *testing.T) {
	store := newFakeOpsStore()
	store.view = dto.OpsView{
		Queues:       []dto.QueueDepth{{Queue: "reminder_email", Depth: 3, OldestWaitSeconds: 12}},
		Deliveries:   dto.DeliveryCounts{Succeeded: 9, Failed: 1},
		RetryRate:    0.25,
		LatencyP95Ms: 1800,
		CheckedAt:    fixedNow,
	}
	handler := &ReminderOpsHandler{Ops: store, Now: func() time.Time { return fixedNow }}

	view, err := handler.Handle(context.Background())
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !reflect.DeepEqual(view, store.view) {
		t.Fatalf("view = %#v, want %#v", view, store.view)
	}
	if len(store.calls) != 1 {
		t.Fatalf("ops calls = %d, want 1", len(store.calls))
	}
	call := store.calls[0]
	if !call.now.Equal(fixedNow) || call.window != 24*time.Hour {
		t.Fatalf("ops call = %#v, want fixed clock and 24h window", call)
	}
}

func TestReminderOpsPropagatesStoreError(t *testing.T) {
	store := newFakeOpsStore()
	store.err = errStatsFailed
	handler := &ReminderOpsHandler{Ops: store, Now: func() time.Time { return fixedNow }}

	if _, err := handler.Handle(context.Background()); !errors.Is(err, errStatsFailed) {
		t.Fatalf("Handle() error = %v, want errStatsFailed", err)
	}
}

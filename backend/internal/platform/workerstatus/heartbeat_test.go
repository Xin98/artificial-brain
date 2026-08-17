package workerstatus

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestHeartbeatRecordsImmediatelyOnEachTickAndRemovesOnCancel(t *testing.T) {
	recorder := newFakeRecorder()
	ticks := newFakeTicks()
	instance := Instance{ID: "worker-1", Version: "abc", StartedAt: time.Date(2026, 8, 13, 3, 0, 0, 0, time.UTC)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- NewHeartbeat(recorder, instance, ticks).Run(ctx) }()

	recorder.waitForRecords(t, 1)
	ticks.send(t, time.Date(2026, 8, 13, 3, 1, 0, 0, time.UTC))
	recorder.waitForRecords(t, 2)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := recorder.removed(); got != instance.ID {
		t.Fatalf("removed ID = %q, want %q", got, instance.ID)
	}
	if !recorder.removeDeadlineWasCapped() {
		t.Fatal("Remove() context did not have a two-second-or-less deadline")
	}
	if ticks.stopCount() != 1 {
		t.Fatalf("TickSource.Stop() calls = %d, want 1", ticks.stopCount())
	}
}

func TestHeartbeatReturnsRecordFailureAndStopsTicks(t *testing.T) {
	wantErr := errors.New("database unavailable")
	recorder := newFakeRecorder()
	recorder.recordErr = wantErr
	ticks := newFakeTicks()
	done := make(chan error, 1)
	go func() {
		done <- NewHeartbeat(recorder, Instance{ID: "worker-1"}, ticks).Run(context.Background())
	}()

	if err := <-done; !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if got := recorder.removed(); got != "" {
		t.Fatalf("removed ID = %q, want no removal without cancellation", got)
	}
	if ticks.stopCount() != 1 {
		t.Fatalf("TickSource.Stop() calls = %d, want 1", ticks.stopCount())
	}
}

func TestHeartbeatReturnsCleanupFailureOnCancellation(t *testing.T) {
	wantErr := errors.New("remove lease failed")
	recorder := newFakeRecorder()
	recorder.removeErr = wantErr
	ticks := newFakeTicks()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- NewHeartbeat(recorder, Instance{ID: "worker-1"}, ticks).Run(ctx)
	}()

	recorder.waitForRecords(t, 1)
	cancel()
	if err := <-done; !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
}

func TestHeartbeatCleansUpAfterCancelledRecordWithoutReplacingRecordError(t *testing.T) {
	cleanupErr := errors.New("remove lease failed")
	for _, test := range []struct {
		name          string
		blockedRecord int
		sendTick      bool
	}{
		{name: "initial record", blockedRecord: 1},
		{name: "tick record", blockedRecord: 2, sendTick: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := newBlockingRecordRecorder(test.blockedRecord, context.Canceled, cleanupErr)
			ticks := newFakeTicks()
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() {
				done <- NewHeartbeat(recorder, Instance{ID: "worker-1"}, ticks).Run(ctx)
			}()

			if test.sendTick {
				recorder.waitForRecords(t, 1)
				ticks.send(t, time.Now())
			}
			recorder.waitForBlockedRecord(t)
			cancel()
			recorder.releaseBlockedRecord()
			if err := waitForRun(t, done); !errors.Is(err, context.Canceled) {
				t.Fatalf("Run() error = %v, want context.Canceled", err)
			}
			if got := recorder.removed(); got != "worker-1" {
				t.Fatalf("removed ID = %q, want worker-1", got)
			}
		})
	}
}

func TestHeartbeatPrioritizesCancellationOverPendingTick(t *testing.T) {
	for range 1 {
		recorder := newGatedFirstRecordRecorder()
		ticks := newBufferedFakeTicks()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- NewHeartbeat(recorder, Instance{ID: "worker-1"}, ticks).Run(ctx)
		}()

		recorder.waitForFirstRecordEntry(t)
		ticks.send(t, time.Now())
		cancel()
		recorder.releaseFirstRecord()
		if err := waitForRun(t, done); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if got := recorder.recordCount(); got != 1 {
			t.Fatalf("Record() calls = %d, want 1 when cancellation and tick are both ready", got)
		}
		if got := recorder.removed(); got != "worker-1" {
			t.Fatalf("removed ID = %q, want worker-1", got)
		}
	}
}

func TestNewTimeTickSourceRejectsNonPositiveInterval(t *testing.T) {
	if _, err := NewTimeTickSource(0); err == nil {
		t.Fatal("NewTimeTickSource(0) error = nil, want error")
	}
}

type fakeRecorder struct {
	mu                   sync.Mutex
	recorded             chan struct{}
	records              []Instance
	recordErr            error
	recordErrCall        int
	removeErr            error
	firstRecordEntered   chan struct{}
	firstRecordRelease   chan struct{}
	blockedRecordCall    int
	blockedRecordEntered chan struct{}
	blockedRecordRelease chan struct{}
	removedID            string
	removeHasDeadline    bool
	removeDeadline       time.Time
}

func newFakeRecorder() *fakeRecorder { return &fakeRecorder{recorded: make(chan struct{}, 16)} }

func newGatedFirstRecordRecorder() *fakeRecorder {
	return &fakeRecorder{
		recorded:           make(chan struct{}, 16),
		firstRecordEntered: make(chan struct{}, 1),
		firstRecordRelease: make(chan struct{}),
	}
}

func newBlockingRecordRecorder(call int, recordErr, removeErr error) *fakeRecorder {
	return &fakeRecorder{
		recorded:             make(chan struct{}, 16),
		recordErr:            recordErr,
		recordErrCall:        call,
		removeErr:            removeErr,
		blockedRecordCall:    call,
		blockedRecordEntered: make(chan struct{}, 1),
		blockedRecordRelease: make(chan struct{}),
	}
}

func (r *fakeRecorder) Record(_ context.Context, instance Instance) error {
	r.mu.Lock()
	r.records = append(r.records, instance)
	call := len(r.records)
	err := r.recordErr
	if r.recordErrCall != 0 && call != r.recordErrCall {
		err = nil
	}
	firstRecordGate := call == 1 && r.firstRecordRelease != nil
	blockedRecordGate := call == r.blockedRecordCall && r.blockedRecordRelease != nil
	r.mu.Unlock()
	r.recorded <- struct{}{}
	if firstRecordGate {
		r.firstRecordEntered <- struct{}{}
		<-r.firstRecordRelease
	}
	if blockedRecordGate {
		r.blockedRecordEntered <- struct{}{}
		<-r.blockedRecordRelease
	}
	return err
}

func (r *fakeRecorder) waitForFirstRecordEntry(t *testing.T) {
	t.Helper()
	select {
	case <-r.firstRecordEntered:
	case <-time.After(time.Second):
		t.Fatal("initial Record() did not enter its gate")
	}
}

func (r *fakeRecorder) releaseFirstRecord() { close(r.firstRecordRelease) }

func (r *fakeRecorder) waitForBlockedRecord(t *testing.T) {
	t.Helper()
	select {
	case <-r.blockedRecordEntered:
	case <-time.After(time.Second):
		t.Fatal("Record() did not enter its cancellation gate")
	}
}

func (r *fakeRecorder) releaseBlockedRecord() { close(r.blockedRecordRelease) }

func (r *fakeRecorder) Remove(ctx context.Context, id string) error {
	deadline, hasDeadline := ctx.Deadline()
	r.mu.Lock()
	r.removedID = id
	r.removeHasDeadline = hasDeadline
	r.removeDeadline = deadline
	err := r.removeErr
	r.mu.Unlock()
	return err
}

func (r *fakeRecorder) waitForRecords(t *testing.T, want int) {
	t.Helper()
	for {
		r.mu.Lock()
		got := len(r.records)
		r.mu.Unlock()
		if got >= want {
			return
		}
		select {
		case <-r.recorded:
		case <-time.After(time.Second):
			t.Fatalf("Record() calls = %d, want at least %d", got, want)
		}
	}
}

func (r *fakeRecorder) removed() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.removedID
}

func (r *fakeRecorder) recordCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

func (r *fakeRecorder) removeDeadlineWasCapped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.removeHasDeadline && time.Until(r.removeDeadline) <= 2*time.Second
}

type fakeTicks struct {
	ch    chan time.Time
	mu    sync.Mutex
	stops int
}

func newFakeTicks() *fakeTicks { return &fakeTicks{ch: make(chan time.Time)} }

func newBufferedFakeTicks() *fakeTicks { return &fakeTicks{ch: make(chan time.Time, 1)} }

func (t *fakeTicks) C() <-chan time.Time { return t.ch }

func (t *fakeTicks) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stops++
}

func (t *fakeTicks) send(tb testing.TB, tick time.Time) {
	tb.Helper()
	select {
	case t.ch <- tick:
	case <-time.After(time.Second):
		tb.Fatal("tick was not received")
	}
}

func (t *fakeTicks) stopCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stops
}

func waitForRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("Heartbeat.Run() did not return")
		return nil
	}
}

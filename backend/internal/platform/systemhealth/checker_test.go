package systemhealth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/platform/observability"
	"github.com/Xin98/artificial-brain/backend/internal/platform/workerstatus"
)

const contractProbePrefix = "systemhealth-contract-probe:"

func TestContractRepresentativeReportProbe(t *testing.T) {
	if os.Getenv("SYSTEM_HEALTH_CONTRACT_PROBE") != "1" {
		return
	}
	checkedAt := time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)
	report := Report{
		Status: StatusHealthy, CheckedAt: checkedAt, CorrelationID: "req-1",
		Components: map[string]Component{
			"api":      {Status: StatusHealthy, CheckedAt: checkedAt},
			"database": {Status: StatusHealthy, CheckedAt: checkedAt},
			"worker":   {Status: StatusHealthy, CheckedAt: checkedAt},
		},
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(contractProbePrefix + string(encoded))
}

func TestCheckerReportsHealthyComponentsAndCorrelationID(t *testing.T) {
	now := time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)
	report := NewChecker(
		fakeDB{},
		fakeWorkers{lease: workerstatus.Lease{LastHeartbeatAt: now.Add(-time.Second)}},
		func() time.Time { return now },
		6*time.Second,
	).Check(observability.WithCorrelationID(context.Background(), "req-1"))

	if report.Status != StatusHealthy || !report.CheckedAt.Equal(now) || report.CorrelationID != "req-1" {
		t.Fatalf("report = %#v, want healthy report at now with correlation ID", report)
	}
	for _, name := range []string{"api", "database", "worker"} {
		component, ok := report.Components[name]
		if !ok || component.Status != StatusHealthy || !component.CheckedAt.Equal(now) || component.Detail != "" {
			t.Fatalf("component %q = %#v, want healthy check at now without detail", name, component)
		}
	}
}

func TestCheckerMarksDatabaseFailureUnavailable(t *testing.T) {
	now := time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)
	report := NewChecker(fakeDB{err: errors.New("database went away")}, fakeWorkers{lease: workerstatus.Lease{LastHeartbeatAt: now}}, func() time.Time { return now }, 6*time.Second).Check(context.Background())

	if report.Status != StatusDegraded {
		t.Fatalf("status = %q, want %q", report.Status, StatusDegraded)
	}
	if got := report.Components["database"]; got.Status != StatusUnavailable || got.Detail != "database unavailable" {
		t.Fatalf("database = %#v, want unavailable redacted detail", got)
	}
}

func TestCheckerMarksMissingWorkerUnavailable(t *testing.T) {
	now := time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)
	report := NewChecker(fakeDB{}, fakeWorkers{err: workerstatus.ErrNoLease}, func() time.Time { return now }, 6*time.Second).Check(context.Background())

	if report.Status != StatusDegraded {
		t.Fatalf("status = %q, want %q", report.Status, StatusDegraded)
	}
	if got := report.Components["worker"]; got.Status != StatusUnavailable || got.Detail != "worker heartbeat unavailable" {
		t.Fatalf("worker = %#v, want unavailable heartbeat detail", got)
	}
}

func TestCheckerKeepsLeaseAtTTLHealthy(t *testing.T) {
	now := time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)
	report := NewChecker(fakeDB{}, fakeWorkers{lease: workerstatus.Lease{LastHeartbeatAt: now.Add(-6 * time.Second)}}, func() time.Time { return now }, 6*time.Second).Check(context.Background())

	if got := report.Components["worker"]; got.Status != StatusHealthy {
		t.Fatalf("worker = %#v, want healthy at TTL boundary", got)
	}
}

func TestCheckerMarksExpiredWorkerUnavailable(t *testing.T) {
	now := time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)
	checker := NewChecker(
		fakeDB{},
		fakeWorkers{lease: workerstatus.Lease{LastHeartbeatAt: now.Add(-6*time.Second - time.Nanosecond)}},
		func() time.Time { return now },
		6*time.Second,
	)
	report := checker.Check(observability.WithCorrelationID(context.Background(), "req-1"))
	if report.Status != StatusDegraded {
		t.Fatalf("status = %q", report.Status)
	}
	if report.Components["worker"].Status != StatusUnavailable {
		t.Fatalf("worker = %#v", report.Components["worker"])
	}
}

func TestCheckerClampsNegativeWorkerLeaseAge(t *testing.T) {
	now := time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC)
	report := NewChecker(fakeDB{}, fakeWorkers{lease: workerstatus.Lease{LastHeartbeatAt: now.Add(time.Nanosecond)}}, func() time.Time { return now }, 6*time.Second).Check(context.Background())

	if got := report.Components["worker"]; got.Status != StatusHealthy {
		t.Fatalf("worker = %#v, want healthy for negative clock skew", got)
	}
}

func TestCheckerNormalizesCheckedAtToUTCAndMarshalsRFC3339(t *testing.T) {
	local := time.Date(2026, 8, 13, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	report := NewChecker(
		fakeDB{},
		fakeWorkers{lease: workerstatus.Lease{LastHeartbeatAt: local.Add(-time.Second)}},
		func() time.Time { return local },
		6*time.Second,
	).Check(context.Background())

	if report.CheckedAt.Location() != time.UTC || !report.CheckedAt.Equal(local.UTC()) {
		t.Fatalf("report checkedAt = %s (%s), want %s UTC", report.CheckedAt, report.CheckedAt.Location(), local.UTC())
	}
	for _, name := range []string{"api", "database", "worker"} {
		if got := report.Components[name].CheckedAt; got.Location() != time.UTC || !got.Equal(local.UTC()) {
			t.Fatalf("%s checkedAt = %s (%s), want %s UTC", name, got, got.Location(), local.UTC())
		}
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"checkedAt":"2026-08-13T04:00:00Z"`)) {
		t.Fatalf("checkedAt JSON = %s, want RFC3339 UTC", encoded)
	}
}

func TestCheckerRedactsDependencyErrors(t *testing.T) {
	checker := NewChecker(fakeDB{err: errors.New("postgres://user:secret@db")}, fakeWorkers{err: errors.New("worker secret")}, fixedClock, 6*time.Second)
	report := checker.Check(context.Background())
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("secret")) {
		t.Fatalf("leaked: %s", encoded)
	}
}

func fixedClock() time.Time { return time.Date(2026, 8, 13, 4, 0, 0, 0, time.UTC) }

type fakeDB struct{ err error }

func (db fakeDB) Ping(context.Context) error { return db.err }

type fakeWorkers struct {
	lease workerstatus.Lease
	err   error
}

func (workers fakeWorkers) Latest(context.Context) (workerstatus.Lease, error) {
	return workers.lease, workers.err
}

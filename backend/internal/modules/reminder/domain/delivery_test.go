package domain

import (
	"errors"
	"testing"
	"time"
)

// testNow is shared with plan_test.go.

// deliveryArgs bundles the constructor inputs so rejection cases can mutate a
// single field against an otherwise valid baseline.
type deliveryArgs struct {
	id                  string
	workspaceID         string
	ownerUserID         string
	todoID              string
	todoReminderVersion int
	planID              string
	channel             string
	titleSnapshot       string
	scheduledAt         time.Time
	now                 time.Time
}

func defaultDeliveryArgs() deliveryArgs {
	return deliveryArgs{
		id:                  "del-1",
		workspaceID:         "ws-1",
		ownerUserID:         "user-1",
		todoID:              "todo-1",
		todoReminderVersion: 2,
		planID:              "plan-1",
		channel:             "email",
		titleSnapshot:       "buy milk",
		scheduledAt:         testNow.Add(time.Hour),
		now:                 testNow,
	}
}

func (a deliveryArgs) build() (ReminderDelivery, error) {
	return NewDelivery(a.id, a.workspaceID, a.ownerUserID, a.todoID, a.todoReminderVersion, a.planID, a.channel, a.titleSnapshot, a.scheduledAt, a.now)
}

func newTestDelivery(t *testing.T) ReminderDelivery {
	t.Helper()
	delivery, err := defaultDeliveryArgs().build()
	if err != nil {
		t.Fatalf("NewDelivery() error = %v", err)
	}
	return delivery
}

func newSucceededDelivery(t *testing.T) ReminderDelivery {
	t.Helper()
	delivery := newTestDelivery(t)
	if err := delivery.MarkSending(testNow); err != nil {
		t.Fatalf("MarkSending() error = %v", err)
	}
	if err := delivery.MarkSucceeded("msg-1", testNow.Add(time.Minute)); err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}
	return delivery
}

func TestIdempotencyKeyForFormat(t *testing.T) {
	if got := IdempotencyKeyFor("ws-1", "todo-1", 3, "sms"); got != "ws-1:todo-1:3:sms" {
		t.Fatalf("IdempotencyKeyFor() = %q, want %q", got, "ws-1:todo-1:3:sms")
	}
}

func TestNewDeliveryNormalizesToScheduled(t *testing.T) {
	delivery, err := defaultDeliveryArgs().build()
	if err != nil {
		t.Fatalf("NewDelivery() error = %v", err)
	}
	if delivery.ID != "del-1" || delivery.WorkspaceID != "ws-1" || delivery.OwnerUserID != "user-1" || delivery.TodoID != "todo-1" {
		t.Fatalf("delivery identity = %#v", delivery)
	}
	if delivery.TodoReminderVersion != 2 {
		t.Fatalf("delivery.TodoReminderVersion = %d, want 2", delivery.TodoReminderVersion)
	}
	if delivery.PlanID != "plan-1" || delivery.Channel != "email" || delivery.TodoTitleSnapshot != "buy milk" {
		t.Fatalf("delivery plan fields = %#v", delivery)
	}
	if delivery.IdempotencyKey != "ws-1:todo-1:2:email" {
		t.Fatalf("delivery.IdempotencyKey = %q, want %q", delivery.IdempotencyKey, "ws-1:todo-1:2:email")
	}
	if delivery.State != StateScheduled {
		t.Fatalf("delivery.State = %q, want %q", delivery.State, StateScheduled)
	}
	if !delivery.ScheduledAt.Equal(testNow.Add(time.Hour)) {
		t.Fatalf("delivery.ScheduledAt = %v", delivery.ScheduledAt)
	}
	if !delivery.CreatedAt.Equal(testNow) {
		t.Fatalf("delivery.CreatedAt = %v", delivery.CreatedAt)
	}
	if delivery.AttemptCount != 0 {
		t.Fatalf("delivery.AttemptCount = %d, want 0", delivery.AttemptCount)
	}
	if delivery.SuppressionReason != nil || delivery.ProviderJobID != nil || delivery.ProviderMessageID != nil || delivery.LastErrorCode != nil {
		t.Fatalf("delivery optional provider fields not nil = %#v", delivery)
	}
	if delivery.SubmittedAt != nil || delivery.FinalizedAt != nil || delivery.ReceiptState != nil || delivery.ReceiptAt != nil || delivery.ReceiptErrorCode != nil {
		t.Fatalf("delivery optional receipt fields not nil = %#v", delivery)
	}
}

func TestNewDeliveryAcceptsKnownChannels(t *testing.T) {
	for _, channel := range []string{"email", "sms"} {
		args := defaultDeliveryArgs()
		args.channel = channel
		delivery, err := args.build()
		if err != nil {
			t.Fatalf("NewDelivery(channel %q) error = %v", channel, err)
		}
		if delivery.Channel != channel {
			t.Fatalf("delivery.Channel = %q, want %q", delivery.Channel, channel)
		}
	}
}

func TestNewDeliveryRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*deliveryArgs)
		wantErr error
	}{
		{"empty id", func(a *deliveryArgs) { a.id = "" }, ErrMissingDeliveryFields},
		{"empty workspace", func(a *deliveryArgs) { a.workspaceID = "" }, ErrMissingDeliveryFields},
		{"empty owner", func(a *deliveryArgs) { a.ownerUserID = "" }, ErrMissingDeliveryFields},
		{"empty todo", func(a *deliveryArgs) { a.todoID = "" }, ErrMissingDeliveryFields},
		{"empty plan", func(a *deliveryArgs) { a.planID = "" }, ErrMissingDeliveryFields},
		{"empty title", func(a *deliveryArgs) { a.titleSnapshot = "" }, ErrMissingDeliveryFields},
		{"zero scheduled at", func(a *deliveryArgs) { a.scheduledAt = time.Time{} }, ErrMissingSchedule},
		{"unknown channel", func(a *deliveryArgs) { a.channel = "push" }, ErrInvalidDeliveryChannel},
		{"empty channel", func(a *deliveryArgs) { a.channel = "" }, ErrInvalidDeliveryChannel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := defaultDeliveryArgs()
			tc.mutate(&args)
			if _, err := args.build(); !errors.Is(err, tc.wantErr) {
				t.Fatalf("NewDelivery() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestMarkSendingIncrementsAttempts(t *testing.T) {
	delivery := newTestDelivery(t)
	if err := delivery.MarkSending(testNow.Add(time.Minute)); err != nil {
		t.Fatalf("MarkSending() from scheduled error = %v", err)
	}
	if delivery.State != StateSending || delivery.AttemptCount != 1 {
		t.Fatalf("delivery after first MarkSending = state %q attempts %d, want %q attempts 1", delivery.State, delivery.AttemptCount, StateSending)
	}
	if err := delivery.MarkSending(testNow.Add(2 * time.Minute)); err != nil {
		t.Fatalf("MarkSending() from sending error = %v", err)
	}
	if delivery.State != StateSending || delivery.AttemptCount != 2 {
		t.Fatalf("delivery after second MarkSending = state %q attempts %d, want %q attempts 2", delivery.State, delivery.AttemptCount, StateSending)
	}
}

func TestIsFinalOnlyForTerminalStates(t *testing.T) {
	delivery := newTestDelivery(t)
	if delivery.IsFinal() {
		t.Fatal("scheduled delivery reports final")
	}
	if err := delivery.MarkSending(testNow); err != nil {
		t.Fatalf("MarkSending() error = %v", err)
	}
	if delivery.IsFinal() {
		t.Fatal("sending delivery reports final")
	}
}

func newDeliveryInFinalState(t *testing.T, state DeliveryState) ReminderDelivery {
	t.Helper()
	delivery := newTestDelivery(t)
	switch state {
	case StateSucceeded:
		if err := delivery.MarkSending(testNow); err != nil {
			t.Fatalf("MarkSending() error = %v", err)
		}
		if err := delivery.MarkSucceeded("msg-1", testNow.Add(time.Minute)); err != nil {
			t.Fatalf("MarkSucceeded() error = %v", err)
		}
	case StateFailed:
		if err := delivery.MarkFailed("boom", testNow.Add(time.Minute)); err != nil {
			t.Fatalf("MarkFailed() error = %v", err)
		}
	case StateSuppressed:
		if err := delivery.MarkSuppressed(ReasonPlanRevoked, testNow.Add(time.Minute)); err != nil {
			t.Fatalf("MarkSuppressed() error = %v", err)
		}
	default:
		t.Fatalf("unsupported final state %q", state)
	}
	return delivery
}

func TestTransitionsFromFinalStatesReturnErrDeliveryFinal(t *testing.T) {
	transitions := map[string]func(*ReminderDelivery) error{
		"MarkSending":    func(d *ReminderDelivery) error { return d.MarkSending(testNow) },
		"MarkSucceeded":  func(d *ReminderDelivery) error { return d.MarkSucceeded("msg-9", testNow) },
		"MarkFailed":     func(d *ReminderDelivery) error { return d.MarkFailed("code", testNow) },
		"MarkSuppressed": func(d *ReminderDelivery) error { return d.MarkSuppressed(ReasonTodoDeleted, testNow) },
	}
	for _, state := range []DeliveryState{StateSucceeded, StateFailed, StateSuppressed} {
		for name, transition := range transitions {
			t.Run(string(state)+"/"+name, func(t *testing.T) {
				delivery := newDeliveryInFinalState(t, state)
				if err := transition(&delivery); !errors.Is(err, ErrDeliveryFinal) {
					t.Fatalf("%s() on %s delivery error = %v, want ErrDeliveryFinal", name, state, err)
				}
				if delivery.State != state {
					t.Fatalf("rejected %s() changed state to %q, want %q", name, delivery.State, state)
				}
			})
		}
	}
}

func TestMarkSucceededSetsTimestamps(t *testing.T) {
	delivery := newTestDelivery(t)
	if err := delivery.MarkSending(testNow); err != nil {
		t.Fatalf("MarkSending() error = %v", err)
	}
	submitted := testNow.Add(5 * time.Minute)
	if err := delivery.MarkSucceeded("msg-42", submitted); err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}
	if delivery.State != StateSucceeded || !delivery.IsFinal() {
		t.Fatalf("delivery after MarkSucceeded = state %q final %v", delivery.State, delivery.IsFinal())
	}
	if delivery.ProviderMessageID == nil || *delivery.ProviderMessageID != "msg-42" {
		t.Fatalf("delivery.ProviderMessageID = %v, want msg-42", delivery.ProviderMessageID)
	}
	if delivery.SubmittedAt == nil || !delivery.SubmittedAt.Equal(submitted) {
		t.Fatalf("delivery.SubmittedAt = %v, want %v", delivery.SubmittedAt, submitted)
	}
	if delivery.FinalizedAt == nil || !delivery.FinalizedAt.Equal(submitted) {
		t.Fatalf("delivery.FinalizedAt = %v, want %v", delivery.FinalizedAt, submitted)
	}
}

func TestMarkSucceededRequiresSending(t *testing.T) {
	delivery := newTestDelivery(t)
	if err := delivery.MarkSucceeded("msg-42", testNow); !errors.Is(err, ErrDeliveryNotSending) {
		t.Fatalf("MarkSucceeded() from scheduled error = %v, want ErrDeliveryNotSending", err)
	}
	if delivery.State != StateScheduled {
		t.Fatalf("rejected MarkSucceeded() changed state to %q", delivery.State)
	}
}

func TestMarkFailedRecordsErrorCode(t *testing.T) {
	delivery := newTestDelivery(t)
	if err := delivery.MarkSending(testNow); err != nil {
		t.Fatalf("MarkSending() error = %v", err)
	}
	finalized := testNow.Add(3 * time.Minute)
	if err := delivery.MarkFailed("provider_timeout", finalized); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}
	if delivery.State != StateFailed || !delivery.IsFinal() {
		t.Fatalf("delivery after MarkFailed = state %q final %v", delivery.State, delivery.IsFinal())
	}
	if delivery.LastErrorCode == nil || *delivery.LastErrorCode != "provider_timeout" {
		t.Fatalf("delivery.LastErrorCode = %v, want provider_timeout", delivery.LastErrorCode)
	}
	if delivery.FinalizedAt == nil || !delivery.FinalizedAt.Equal(finalized) {
		t.Fatalf("delivery.FinalizedAt = %v, want %v", delivery.FinalizedAt, finalized)
	}
}

func TestMarkFailedAllowedFromScheduled(t *testing.T) {
	delivery := newTestDelivery(t)
	if err := delivery.MarkFailed("dead_on_arrival", testNow); err != nil {
		t.Fatalf("MarkFailed() from scheduled error = %v", err)
	}
	if delivery.State != StateFailed {
		t.Fatalf("delivery.State = %q, want %q", delivery.State, StateFailed)
	}
}

func TestMarkSuppressedRecordsReason(t *testing.T) {
	delivery := newTestDelivery(t)
	finalized := testNow.Add(time.Minute)
	if err := delivery.MarkSuppressed(ReasonTodoCompleted, finalized); err != nil {
		t.Fatalf("MarkSuppressed() error = %v", err)
	}
	if delivery.State != StateSuppressed || !delivery.IsFinal() {
		t.Fatalf("delivery after MarkSuppressed = state %q final %v", delivery.State, delivery.IsFinal())
	}
	if delivery.SuppressionReason == nil || *delivery.SuppressionReason != ReasonTodoCompleted {
		t.Fatalf("delivery.SuppressionReason = %v, want %q", delivery.SuppressionReason, ReasonTodoCompleted)
	}
	if delivery.FinalizedAt == nil || !delivery.FinalizedAt.Equal(finalized) {
		t.Fatalf("delivery.FinalizedAt = %v, want %v", delivery.FinalizedAt, finalized)
	}
}

func TestApplyReceiptOnceOnSucceeded(t *testing.T) {
	delivery := newSucceededDelivery(t)
	received := testNow.Add(10 * time.Minute)
	if err := delivery.ApplyReceipt(ReceiptOK, "", received); err != nil {
		t.Fatalf("ApplyReceipt() error = %v", err)
	}
	if delivery.ReceiptState == nil || *delivery.ReceiptState != ReceiptOK {
		t.Fatalf("delivery.ReceiptState = %v, want %q", delivery.ReceiptState, ReceiptOK)
	}
	if delivery.ReceiptAt == nil || !delivery.ReceiptAt.Equal(received) {
		t.Fatalf("delivery.ReceiptAt = %v, want %v", delivery.ReceiptAt, received)
	}
	if delivery.ReceiptErrorCode != nil {
		t.Fatalf("delivery.ReceiptErrorCode = %v, want nil for an ok receipt", *delivery.ReceiptErrorCode)
	}
	if err := delivery.ApplyReceipt(ReceiptFailed, "later", received.Add(time.Minute)); err != nil {
		t.Fatalf("second ApplyReceipt() error = %v, want nil", err)
	}
	if delivery.ReceiptState == nil || *delivery.ReceiptState != ReceiptOK || delivery.ReceiptAt == nil || !delivery.ReceiptAt.Equal(received) || delivery.ReceiptErrorCode != nil {
		t.Fatalf("second ApplyReceipt() mutated the receipt = %#v", delivery)
	}
}

func TestApplyReceiptRecordsErrorCode(t *testing.T) {
	delivery := newSucceededDelivery(t)
	if err := delivery.ApplyReceipt(ReceiptFailed, "invalid_number", testNow.Add(10*time.Minute)); err != nil {
		t.Fatalf("ApplyReceipt() error = %v", err)
	}
	if delivery.ReceiptState == nil || *delivery.ReceiptState != ReceiptFailed {
		t.Fatalf("delivery.ReceiptState = %v, want %q", delivery.ReceiptState, ReceiptFailed)
	}
	if delivery.ReceiptErrorCode == nil || *delivery.ReceiptErrorCode != "invalid_number" {
		t.Fatalf("delivery.ReceiptErrorCode = %v, want invalid_number", delivery.ReceiptErrorCode)
	}
}

func TestApplyReceiptRejectedOutsideSucceeded(t *testing.T) {
	cases := []struct {
		name  string
		build func(*testing.T) ReminderDelivery
	}{
		{"scheduled", func(t *testing.T) ReminderDelivery { return newTestDelivery(t) }},
		{"sending", func(t *testing.T) ReminderDelivery {
			delivery := newTestDelivery(t)
			if err := delivery.MarkSending(testNow); err != nil {
				t.Fatalf("MarkSending() error = %v", err)
			}
			return delivery
		}},
		{"failed", func(t *testing.T) ReminderDelivery {
			delivery := newTestDelivery(t)
			if err := delivery.MarkFailed("boom", testNow); err != nil {
				t.Fatalf("MarkFailed() error = %v", err)
			}
			return delivery
		}},
		{"suppressed", func(t *testing.T) ReminderDelivery {
			delivery := newTestDelivery(t)
			if err := delivery.MarkSuppressed(ReasonVersionStale, testNow); err != nil {
				t.Fatalf("MarkSuppressed() error = %v", err)
			}
			return delivery
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			delivery := tc.build(t)
			if err := delivery.ApplyReceipt(ReceiptOK, "", testNow); !errors.Is(err, ErrReceiptNotApplicable) {
				t.Fatalf("ApplyReceipt() on %s delivery error = %v, want ErrReceiptNotApplicable", tc.name, err)
			}
			if delivery.ReceiptState != nil || delivery.ReceiptAt != nil {
				t.Fatalf("rejected ApplyReceipt() recorded a receipt on %s delivery", tc.name)
			}
		})
	}
}

// restoreDeliveryArgs bundles the RestoreDelivery inputs so rejection cases
// can mutate a single field against an otherwise valid baseline.
type restoreDeliveryArgs struct {
	id                  string
	workspaceID         string
	ownerUserID         string
	todoID              string
	todoReminderVersion int
	channel             string
	titleSnapshot       string
	idempotencyKey      string
	state               DeliveryState
	suppressionReason   *SuppressionReason
	attemptCount        int
	providerMessageID   *string
	lastErrorCode       *string
	scheduledAt         time.Time
	createdAt           time.Time
	submittedAt         *time.Time
	finalizedAt         *time.Time
	receiptState        *ReceiptState
	receiptAt           *time.Time
	receiptErrorCode    *string
}

func defaultRestoreDeliveryArgs() restoreDeliveryArgs {
	messageID := "provider-message-7"
	submitted := testNow.Add(5 * time.Minute)
	finalized := submitted
	return restoreDeliveryArgs{
		id:                  "del-9",
		workspaceID:         "ws-1",
		ownerUserID:         "user-1",
		todoID:              "todo-1",
		todoReminderVersion: 2,
		channel:             "sms",
		titleSnapshot:       "buy milk",
		idempotencyKey:      "import:instance-a:record-1",
		state:               StateSucceeded,
		attemptCount:        1,
		providerMessageID:   &messageID,
		scheduledAt:         testNow.Add(time.Hour),
		createdAt:           testNow,
		submittedAt:         &submitted,
		finalizedAt:         &finalized,
	}
}

func (a restoreDeliveryArgs) build() (ReminderDelivery, error) {
	return RestoreDelivery(a.id, a.workspaceID, a.ownerUserID, a.todoID, a.todoReminderVersion,
		a.channel, a.titleSnapshot, a.idempotencyKey, a.state,
		a.suppressionReason, a.attemptCount, a.providerMessageID, a.lastErrorCode,
		a.scheduledAt, a.createdAt, a.submittedAt, a.finalizedAt,
		a.receiptState, a.receiptAt, a.receiptErrorCode)
}

func TestRestoreDeliveryRebuildsHistoricalDelivery(t *testing.T) {
	args := defaultRestoreDeliveryArgs()
	receipt := ReceiptOK
	receiptAt := testNow.Add(10 * time.Minute)
	args.receiptState = &receipt
	args.receiptAt = &receiptAt

	delivery, err := args.build()
	if err != nil {
		t.Fatalf("RestoreDelivery() error = %v", err)
	}
	if delivery.ID != "del-9" || delivery.WorkspaceID != "ws-1" || delivery.OwnerUserID != "user-1" || delivery.TodoID != "todo-1" {
		t.Fatalf("delivery identity = %#v", delivery)
	}
	if delivery.TodoReminderVersion != 2 || delivery.Channel != "sms" || delivery.TodoTitleSnapshot != "buy milk" {
		t.Fatalf("delivery history fields = %#v", delivery)
	}
	if delivery.IdempotencyKey != "import:instance-a:record-1" {
		t.Fatalf("delivery.IdempotencyKey = %q, want the caller's import key", delivery.IdempotencyKey)
	}
	if delivery.State != StateSucceeded || delivery.AttemptCount != 1 {
		t.Fatalf("delivery state/attempts = %q/%d, want succeeded/1", delivery.State, delivery.AttemptCount)
	}
	if delivery.ProviderMessageID == nil || *delivery.ProviderMessageID != "provider-message-7" {
		t.Fatalf("delivery.ProviderMessageID = %v, want provider-message-7", delivery.ProviderMessageID)
	}
	if !delivery.ScheduledAt.Equal(testNow.Add(time.Hour)) || !delivery.CreatedAt.Equal(testNow) {
		t.Fatalf("delivery schedule/created = %v/%v", delivery.ScheduledAt, delivery.CreatedAt)
	}
	if delivery.SubmittedAt == nil || !delivery.SubmittedAt.Equal(testNow.Add(5*time.Minute)) || delivery.FinalizedAt == nil || !delivery.FinalizedAt.Equal(testNow.Add(5*time.Minute)) {
		t.Fatalf("delivery submitted/finalized = %v/%v", delivery.SubmittedAt, delivery.FinalizedAt)
	}
	if delivery.ReceiptState == nil || *delivery.ReceiptState != ReceiptOK || delivery.ReceiptAt == nil || !delivery.ReceiptAt.Equal(testNow.Add(10*time.Minute)) || delivery.ReceiptErrorCode != nil {
		t.Fatalf("delivery receipt = %#v", delivery)
	}
	if delivery.PlanID != "" {
		t.Fatalf("delivery.PlanID = %q, want empty for a restored delivery", delivery.PlanID)
	}
	if delivery.Origin != OriginImported {
		t.Fatalf("delivery.Origin = %q, want %q", delivery.Origin, OriginImported)
	}
	if delivery.ProviderJobID != nil || delivery.SuppressionReason != nil || delivery.LastErrorCode != nil {
		t.Fatalf("delivery unset optionals not nil = %#v", delivery)
	}
}

func TestRestoreDeliveryAcceptsEveryState(t *testing.T) {
	for _, state := range []DeliveryState{StateScheduled, StateSending, StateSucceeded, StateFailed, StateSuppressed} {
		t.Run(string(state), func(t *testing.T) {
			args := defaultRestoreDeliveryArgs()
			args.state = state
			if state == StateSuppressed {
				reason := ReasonTodoCompleted
				args.suppressionReason = &reason
			}
			delivery, err := args.build()
			if err != nil {
				t.Fatalf("RestoreDelivery(state %q) error = %v", state, err)
			}
			if delivery.State != state {
				t.Fatalf("delivery.State = %q, want %q", delivery.State, state)
			}
		})
	}
}

func TestRestoreDeliveryRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*restoreDeliveryArgs)
		wantErr error
	}{
		{"empty id", func(a *restoreDeliveryArgs) { a.id = "" }, ErrMissingDeliveryFields},
		{"empty workspace", func(a *restoreDeliveryArgs) { a.workspaceID = "" }, ErrMissingDeliveryFields},
		{"empty owner", func(a *restoreDeliveryArgs) { a.ownerUserID = "" }, ErrMissingDeliveryFields},
		{"empty todo", func(a *restoreDeliveryArgs) { a.todoID = "" }, ErrMissingDeliveryFields},
		{"empty title snapshot", func(a *restoreDeliveryArgs) { a.titleSnapshot = "" }, ErrMissingDeliveryFields},
		{"empty idempotency key", func(a *restoreDeliveryArgs) { a.idempotencyKey = "" }, ErrMissingDeliveryFields},
		{"unknown channel", func(a *restoreDeliveryArgs) { a.channel = "push" }, ErrInvalidDeliveryChannel},
		{"empty channel", func(a *restoreDeliveryArgs) { a.channel = "" }, ErrInvalidDeliveryChannel},
		{"negative attempt count", func(a *restoreDeliveryArgs) { a.attemptCount = -1 }, ErrInvalidDeliveryAttemptCount},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := defaultRestoreDeliveryArgs()
			tc.mutate(&args)
			if _, err := args.build(); !errors.Is(err, tc.wantErr) {
				t.Fatalf("RestoreDelivery() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestNewDeliveryLeavesOriginLocal(t *testing.T) {
	delivery := newTestDelivery(t)
	// The zero value is the local origin; NewDelivery must never stamp the
	// imported origin, and existing construction sites stay untouched.
	if delivery.Origin == OriginImported {
		t.Fatalf("NewDelivery() stamped the imported origin")
	}
	if delivery.Origin != "" {
		t.Fatalf("NewDelivery() origin = %q, want the zero value", delivery.Origin)
	}
}

func TestNewSuppressionReasonAcceptsKnownReasons(t *testing.T) {
	cases := []struct {
		input string
		want  SuppressionReason
	}{
		{"todo_completed", ReasonTodoCompleted},
		{"todo_deleted", ReasonTodoDeleted},
		{"version_stale", ReasonVersionStale},
		{"channel_unavailable", ReasonChannelUnavailable},
		{"plan_revoked", ReasonPlanRevoked},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := NewSuppressionReason(tc.input)
			if err != nil {
				t.Fatalf("NewSuppressionReason(%q) error = %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("NewSuppressionReason(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNewSuppressionReasonRejectsUnknownValues(t *testing.T) {
	for _, input := range []string{"", "bogus", "TODO_COMPLETED", "suppressed"} {
		t.Run("input="+input, func(t *testing.T) {
			if _, err := NewSuppressionReason(input); !errors.Is(err, ErrInvalidSuppressionReason) {
				t.Fatalf("NewSuppressionReason(%q) error = %v, want ErrInvalidSuppressionReason", input, err)
			}
		})
	}
}

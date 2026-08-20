package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	identitydto "github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	remindercommand "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/command"
	reminderdto "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	reminderquery "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/query"
	reminderdomain "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

const testSecret = "receipt-secret"

var testPrincipal = identitydto.Principal{UserID: "user-1", WorkspaceID: "ws-1", SessionID: "session-1"}

var testNow = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

// allowAuth injects the test principal, mirroring the session middleware.
func allowAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(identitydto.WithPrincipal(r.Context(), testPrincipal)))
	})
}

// passThroughAuth applies no session at all, so the handler's own
// principal-required check runs.
func passThroughAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

// denyAuth rejects at the middleware, like the real session middleware does
// for a missing or invalid cookie.
func denyAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "unauthenticated"})
	})
}

type listCall struct {
	workspaceID string
	filter      reminderdto.DeliveryFilter
}

// fakeDeliveryStore satisfies the reminder ports.DeliveryStore so the real
// application handlers can run against it. List and ByProviderMessageID
// resolve against deliveries; every Update is recorded.
type fakeDeliveryStore struct {
	deliveries     []reminderdomain.ReminderDelivery
	listCalls      []listCall
	listErr        error
	updated        []reminderdomain.ReminderDelivery
	updateErr      error
	providerIDErr  error
	providerIDCall string
}

func (s *fakeDeliveryStore) Save(context.Context, reminderdomain.ReminderDelivery) error {
	return nil
}

func (s *fakeDeliveryStore) Update(_ context.Context, delivery reminderdomain.ReminderDelivery) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.updated = append(s.updated, delivery)
	return nil
}

func (s *fakeDeliveryStore) ByIdempotencyKey(context.Context, string, string) (reminderdomain.ReminderDelivery, error) {
	return reminderdomain.ReminderDelivery{}, reminderdomain.ErrDeliveryNotFound
}

func (s *fakeDeliveryStore) ByProviderMessageID(_ context.Context, providerMessageID string) (reminderdomain.ReminderDelivery, error) {
	s.providerIDCall = providerMessageID
	if s.providerIDErr != nil {
		return reminderdomain.ReminderDelivery{}, s.providerIDErr
	}
	for _, delivery := range s.deliveries {
		if delivery.ProviderMessageID != nil && *delivery.ProviderMessageID == providerMessageID {
			return delivery, nil
		}
	}
	return reminderdomain.ReminderDelivery{}, reminderdomain.ErrDeliveryNotFound
}

func (s *fakeDeliveryStore) SetProviderJobID(context.Context, string, string, int64) error {
	return nil
}

func (s *fakeDeliveryStore) PlannedJobIDs(context.Context, string, string, int) ([]int64, error) {
	return nil, nil
}

func (s *fakeDeliveryStore) Stats(context.Context, string) (reminderdto.DeliveryCounts, error) {
	return reminderdto.DeliveryCounts{}, nil
}

func (s *fakeDeliveryStore) List(_ context.Context, workspaceID string, filter reminderdto.DeliveryFilter) ([]reminderdomain.ReminderDelivery, error) {
	s.listCalls = append(s.listCalls, listCall{workspaceID, filter})
	return s.deliveries, s.listErr
}

type fakeOpsStore struct {
	view reminderdto.OpsView
	err  error
}

func (s *fakeOpsStore) ReminderOps(context.Context, time.Time, time.Duration) (reminderdto.OpsView, error) {
	return s.view, s.err
}

func newTestHandler(store *fakeDeliveryStore, ops *fakeOpsStore) *Handler {
	return &Handler{
		List:    &reminderquery.ListDeliveriesHandler{Deliveries: store},
		Ops:     &reminderquery.ReminderOpsHandler{Ops: ops, Now: func() time.Time { return testNow }},
		Receipt: &remindercommand.RecordReceiptHandler{Deliveries: store, Log: slog.New(slog.DiscardHandler), Now: func() time.Time { return testNow }},
		Parse:   ParseGenericReceipt,
		Secret:  testSecret,
	}
}

func serve(t *testing.T, h *Handler, auth func(http.Handler) http.Handler, method, target string, headers map[string]string, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	RegisterRoutes(mux, auth, h)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)
	return recorder
}

func decodeEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode body error = %v, body = %s", err, recorder.Body.String())
	}
	return envelope
}

func signBody(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

func ptr[T any](value T) *T { return &value }

func succeededDelivery() reminderdomain.ReminderDelivery {
	delivery := reminderdomain.ReminderDelivery{
		ID:                "delivery-1",
		WorkspaceID:       "ws-1",
		TodoID:            "todo-1",
		OwnerUserID:       "user-1",
		Channel:           "sms",
		TodoTitleSnapshot: "提交周报",
		State:             reminderdomain.StateScheduled,
		ScheduledAt:       time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC),
		CreatedAt:         time.Date(2026, 8, 19, 21, 0, 0, 0, time.UTC),
	}
	submitted := time.Date(2026, 8, 20, 11, 0, 1, 0, time.UTC)
	if err := delivery.MarkSending(testNow); err != nil {
		panic(err)
	}
	if err := delivery.MarkSucceeded("provider-1", submitted); err != nil {
		panic(err)
	}
	if err := delivery.ApplyReceipt(reminderdomain.ReceiptOK, "", time.Date(2026, 8, 20, 11, 0, 5, 0, time.UTC)); err != nil {
		panic(err)
	}
	return delivery
}

func TestListDeliveriesReturnsCamelCaseViews(t *testing.T) {
	full := succeededDelivery()
	full.LastErrorCode = ptr("TRANSIENT_TIMEOUT")
	full.ReceiptErrorCode = ptr("DELIVERED_LATE")
	suppressed := reminderdomain.ReminderDelivery{
		ID:                "delivery-2",
		WorkspaceID:       "ws-1",
		TodoID:            "todo-2",
		OwnerUserID:       "user-1",
		Channel:           "email",
		TodoTitleSnapshot: "买菜",
		State:             reminderdomain.StateScheduled,
		AttemptCount:      0,
		ScheduledAt:       time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC),
		CreatedAt:         time.Date(2026, 8, 19, 21, 0, 0, 0, time.UTC),
	}
	suppressed.State = reminderdomain.StateSuppressed
	suppressed.SuppressionReason = ptr(reminderdomain.ReasonTodoCompleted)
	store := &fakeDeliveryStore{deliveries: []reminderdomain.ReminderDelivery{full, suppressed}}
	handler := newTestHandler(store, &fakeOpsStore{})

	recorder := serve(t, handler, allowAuth, http.MethodGet, "/api/v1/reminders?status=succeeded&limit=2&offset=1", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Deliveries []map[string]any `json:"deliveries"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if len(body.Deliveries) != 2 {
		t.Fatalf("deliveries = %#v, want 2 entries", body.Deliveries)
	}
	first := body.Deliveries[0]
	wantFirst := map[string]any{
		"id":                "delivery-1",
		"todoId":            "todo-1",
		"todoTitle":         "提交周报",
		"channel":           "sms",
		"state":             "succeeded",
		"attemptCount":      float64(1),
		"providerMessageId": "provider-1",
		"lastErrorCode":     "TRANSIENT_TIMEOUT",
		"receiptState":      "received_ok",
		"receiptErrorCode":  "DELIVERED_LATE",
	}
	for key, want := range wantFirst {
		if first[key] != want {
			t.Fatalf("deliveries[0][%s] = %#v, want %#v", key, first[key], want)
		}
	}
	for _, key := range []string{"scheduledAt", "createdAt", "submittedAt", "finalizedAt", "receiptAt"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("deliveries[0] missing %s: %#v", key, first)
		}
	}
	if _, ok := first["suppressionReason"]; ok {
		t.Fatalf("deliveries[0] should omit suppressionReason: %#v", first)
	}
	second := body.Deliveries[1]
	if second["id"] != "delivery-2" || second["state"] != "suppressed" || second["suppressionReason"] != "todo_completed" {
		t.Fatalf("deliveries[1] = %#v", second)
	}
	for _, key := range []string{"providerMessageId", "lastErrorCode", "submittedAt", "finalizedAt", "receiptState", "receiptAt", "receiptErrorCode"} {
		if _, ok := second[key]; ok {
			t.Fatalf("deliveries[1] should omit %s: %#v", key, second)
		}
	}
	if len(store.listCalls) != 1 {
		t.Fatalf("list calls = %d, want 1", len(store.listCalls))
	}
	call := store.listCalls[0]
	if call.workspaceID != "ws-1" {
		t.Fatalf("list workspace = %s, want ws-1", call.workspaceID)
	}
	if call.filter.Status != "succeeded" || call.filter.Limit != 2 || call.filter.Offset != 1 {
		t.Fatalf("list filter = %#v, want succeeded/2/1", call.filter)
	}
}

func TestListDeliveriesAppliesDefaultsAndAcceptsRetrying(t *testing.T) {
	store := &fakeDeliveryStore{}
	handler := newTestHandler(store, &fakeOpsStore{})

	recorder := serve(t, handler, allowAuth, http.MethodGet, "/api/v1/reminders", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"deliveries":[]`) {
		t.Fatalf("empty list body = %s, want deliveries array []", recorder.Body.String())
	}
	if call := store.listCalls[0]; call.filter.Status != "" || call.filter.Limit != 50 || call.filter.Offset != 0 {
		t.Fatalf("default filter = %#v, want empty/50/0", call.filter)
	}

	recorder = serve(t, handler, allowAuth, http.MethodGet, "/api/v1/reminders?status=retrying&limit=200&offset=0", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("retrying status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	if call := store.listCalls[1]; call.filter.Status != "retrying" || call.filter.Limit != 200 {
		t.Fatalf("retrying filter = %#v", call.filter)
	}
}

func TestListDeliveriesRequiresPrincipal(t *testing.T) {
	store := &fakeDeliveryStore{}
	handler := newTestHandler(store, &fakeOpsStore{})

	recorder := serve(t, handler, passThroughAuth, http.MethodGet, "/api/v1/reminders", nil, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if envelope := decodeEnvelope(t, recorder); envelope["code"] != "unauthenticated" {
		t.Fatalf("envelope = %#v, want unauthenticated", envelope)
	}
	if len(store.listCalls) != 0 {
		t.Fatalf("list called without principal: %#v", store.listCalls)
	}

	recorder = serve(t, handler, denyAuth, http.MethodGet, "/api/v1/reminders", nil, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("deny status = %d, want 401", recorder.Code)
	}
}

func TestListDeliveriesRejectsBadParams(t *testing.T) {
	targets := []string{
		"/api/v1/reminders?status=bogus",
		"/api/v1/reminders?limit=0",
		"/api/v1/reminders?limit=201",
		"/api/v1/reminders?limit=abc",
		"/api/v1/reminders?offset=-1",
		"/api/v1/reminders?offset=abc",
	}
	for _, target := range targets {
		store := &fakeDeliveryStore{}
		handler := newTestHandler(store, &fakeOpsStore{})
		recorder := serve(t, handler, allowAuth, http.MethodGet, target, nil, "")
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s status = %d, want 422", target, recorder.Code)
		}
		if envelope := decodeEnvelope(t, recorder); envelope["code"] != "validation_error" {
			t.Fatalf("%s envelope = %#v, want validation_error", target, envelope)
		}
		if len(store.listCalls) != 0 {
			t.Fatalf("%s called store despite invalid params", target)
		}
	}
}

func TestOpsReturnsReminderOpsJSON(t *testing.T) {
	ops := &fakeOpsStore{view: reminderdto.OpsView{
		Queues: []reminderdto.QueueDepth{
			{Queue: "reminder_email", Depth: 3, OldestWaitSeconds: 42},
			{Queue: "reminder_sms", Depth: 0, OldestWaitSeconds: 0},
		},
		Deliveries: reminderdto.DeliveryCounts{
			Scheduled: 5, Sending: 1, Retrying: 2, Succeeded: 10, Failed: 3, Suppressed: 4,
		},
		RetryRate:    0.25,
		LatencyP95Ms: 1200,
		CheckedAt:    testNow,
	}}
	handler := newTestHandler(&fakeDeliveryStore{}, ops)

	recorder := serve(t, handler, allowAuth, http.MethodGet, "/api/v1/ops/reminder", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("ops status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Queues []struct {
			Queue             string `json:"queue"`
			Depth             int    `json:"depth"`
			OldestWaitSeconds int    `json:"oldestWaitSeconds"`
		} `json:"queues"`
		Deliveries struct {
			Scheduled  int `json:"scheduled"`
			Sending    int `json:"sending"`
			Retrying   int `json:"retrying"`
			Succeeded  int `json:"succeeded"`
			Failed     int `json:"failed"`
			Suppressed int `json:"suppressed"`
		} `json:"deliveries"`
		RetryRate    float64 `json:"retryRate"`
		LatencyP95Ms int     `json:"latencyP95Ms"`
		CheckedAt    string  `json:"checkedAt"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error = %v, body = %s", err, recorder.Body.String())
	}
	if len(body.Queues) != 2 || body.Queues[0].Queue != "reminder_email" || body.Queues[0].Depth != 3 || body.Queues[0].OldestWaitSeconds != 42 {
		t.Fatalf("queues = %#v", body.Queues)
	}
	if body.Deliveries.Scheduled != 5 || body.Deliveries.Sending != 1 || body.Deliveries.Retrying != 2 ||
		body.Deliveries.Succeeded != 10 || body.Deliveries.Failed != 3 || body.Deliveries.Suppressed != 4 {
		t.Fatalf("deliveries = %#v", body.Deliveries)
	}
	if body.RetryRate != 0.25 || body.LatencyP95Ms != 1200 {
		t.Fatalf("rates = %#v", body)
	}
	if checkedAt, err := time.Parse(time.RFC3339, body.CheckedAt); err != nil || !checkedAt.Equal(testNow) {
		t.Fatalf("checkedAt = %q, want %v", body.CheckedAt, testNow)
	}
}

func TestOpsEmptyQueuesSerializesAsArray(t *testing.T) {
	ops := &fakeOpsStore{view: reminderdto.OpsView{CheckedAt: testNow}}
	handler := newTestHandler(&fakeDeliveryStore{}, ops)

	recorder := serve(t, handler, allowAuth, http.MethodGet, "/api/v1/ops/reminder", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("ops status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"queues":[]`) {
		t.Fatalf("body = %s, want queues array []", recorder.Body.String())
	}
}

func TestOpsRequiresPrincipal(t *testing.T) {
	handler := newTestHandler(&fakeDeliveryStore{}, &fakeOpsStore{})
	recorder := serve(t, handler, passThroughAuth, http.MethodGet, "/api/v1/ops/reminder", nil, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if envelope := decodeEnvelope(t, recorder); envelope["code"] != "unauthenticated" {
		t.Fatalf("envelope = %#v, want unauthenticated", envelope)
	}
}

func TestReceiptValidSignatureRecordsReceipt(t *testing.T) {
	store := &fakeDeliveryStore{deliveries: []reminderdomain.ReminderDelivery{succeededDelivery()}}
	handler := newTestHandler(store, &fakeOpsStore{})
	body := `{"providerMessageId":"provider-1","delivered":true}`

	recorder := serve(t, handler, denyAuth, http.MethodPost, "/api/v1/webhooks/receipts/sms",
		map[string]string{"X-Receipt-Signature": signBody(testSecret, body)}, body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("receipt status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.TrimSpace(recorder.Body.String()) != "{}" {
		t.Fatalf("receipt body = %q, want {}", recorder.Body.String())
	}
	if store.providerIDCall != "provider-1" {
		t.Fatalf("record receipt provider id = %q, want provider-1", store.providerIDCall)
	}
}

func TestReceiptFailedVerdictAppliesErrorCode(t *testing.T) {
	delivery := succeededDelivery()
	delivery.ReceiptState = nil // first receipt wins; the row above already applied one
	store := &fakeDeliveryStore{deliveries: []reminderdomain.ReminderDelivery{delivery}}
	handler := newTestHandler(store, &fakeOpsStore{})
	body := `{"providerMessageId":"provider-1","delivered":false,"errorCode":"MOBILE_NOT_ACTIVE"}`

	recorder := serve(t, handler, denyAuth, http.MethodPost, "/api/v1/webhooks/receipts/sms",
		map[string]string{"X-Receipt-Signature": signBody(testSecret, body)}, body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("receipt status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(store.updated) != 1 {
		t.Fatalf("updates = %d, want 1", len(store.updated))
	}
	updated := store.updated[0]
	if updated.ReceiptState == nil || *updated.ReceiptState != reminderdomain.ReceiptFailed {
		t.Fatalf("receipt state = %#v, want received_failed", updated.ReceiptState)
	}
	if updated.ReceiptErrorCode == nil || *updated.ReceiptErrorCode != "MOBILE_NOT_ACTIVE" {
		t.Fatalf("receipt error code = %#v, want MOBILE_NOT_ACTIVE", updated.ReceiptErrorCode)
	}
	if updated.ReceiptAt == nil || !updated.ReceiptAt.Equal(testNow) {
		t.Fatalf("receipt at = %v, want %v", updated.ReceiptAt, testNow)
	}
}

func TestReceiptTamperedBodyRejected(t *testing.T) {
	store := &fakeDeliveryStore{deliveries: []reminderdomain.ReminderDelivery{succeededDelivery()}}
	handler := newTestHandler(store, &fakeOpsStore{})
	signature := signBody(testSecret, `{"providerMessageId":"provider-1","delivered":true}`)
	tampered := `{"providerMessageId":"provider-1","delivered":false}`

	recorder := serve(t, handler, denyAuth, http.MethodPost, "/api/v1/webhooks/receipts/sms",
		map[string]string{"X-Receipt-Signature": signature}, tampered)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", recorder.Code, recorder.Body.String())
	}
	if envelope := decodeEnvelope(t, recorder); envelope["code"] != "invalid_signature" {
		t.Fatalf("envelope = %#v, want invalid_signature", envelope)
	}
	if len(store.updated) != 0 || store.providerIDCall != "" {
		t.Fatalf("receipt applied despite tampered body: updated=%d call=%q", len(store.updated), store.providerIDCall)
	}
}

func TestReceiptMissingOrInvalidHeaderRejected(t *testing.T) {
	body := `{"providerMessageId":"provider-1","delivered":true}`
	headers := map[string]string{
		"missing":      "",
		"wrong-secret": signBody("other-secret", body),
		"not-hex":      "zzzz-not-hex",
	}
	for name, signature := range headers {
		store := &fakeDeliveryStore{deliveries: []reminderdomain.ReminderDelivery{succeededDelivery()}}
		handler := newTestHandler(store, &fakeOpsStore{})
		requestHeaders := map[string]string{}
		if signature != "" {
			requestHeaders["X-Receipt-Signature"] = signature
		}
		recorder := serve(t, handler, denyAuth, http.MethodPost, "/api/v1/webhooks/receipts/sms", requestHeaders, body)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want 401, body = %s", name, recorder.Code, recorder.Body.String())
		}
		if envelope := decodeEnvelope(t, recorder); envelope["code"] != "invalid_signature" {
			t.Fatalf("%s envelope = %#v, want invalid_signature", name, envelope)
		}
		if len(store.updated) != 0 {
			t.Fatalf("%s applied receipt despite bad signature", name)
		}
	}
}

func TestReceiptMalformedJSONWithValidSignatureRejected(t *testing.T) {
	store := &fakeDeliveryStore{deliveries: []reminderdomain.ReminderDelivery{succeededDelivery()}}
	handler := newTestHandler(store, &fakeOpsStore{})
	body := `{not-json`

	recorder := serve(t, handler, denyAuth, http.MethodPost, "/api/v1/webhooks/receipts/sms",
		map[string]string{"X-Receipt-Signature": signBody(testSecret, body)}, body)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body = %s", recorder.Code, recorder.Body.String())
	}
	if envelope := decodeEnvelope(t, recorder); envelope["code"] != "validation_error" {
		t.Fatalf("envelope = %#v, want validation_error", envelope)
	}
	if len(store.updated) != 0 {
		t.Fatal("receipt applied despite malformed body")
	}
}

func TestReceiptMissingProviderMessageIDRejected(t *testing.T) {
	store := &fakeDeliveryStore{}
	handler := newTestHandler(store, &fakeOpsStore{})
	body := `{"delivered":true}`

	recorder := serve(t, handler, denyAuth, http.MethodPost, "/api/v1/webhooks/receipts/sms",
		map[string]string{"X-Receipt-Signature": signBody(testSecret, body)}, body)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestReceiptUnknownProviderIDStillAccepted(t *testing.T) {
	store := &fakeDeliveryStore{} // no deliveries seeded
	handler := newTestHandler(store, &fakeOpsStore{})
	body := `{"providerMessageId":"ghost","delivered":true}`

	recorder := serve(t, handler, denyAuth, http.MethodPost, "/api/v1/webhooks/receipts/sms",
		map[string]string{"X-Receipt-Signature": signBody(testSecret, body)}, body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(store.updated) != 0 {
		t.Fatalf("unknown provider id mutated store: %#v", store.updated)
	}
}

func TestReceiptOversizedBodyRejected(t *testing.T) {
	store := &fakeDeliveryStore{}
	handler := newTestHandler(store, &fakeOpsStore{})
	body := `{"providerMessageId":"provider-1","padding":"` + strings.Repeat("x", 64*1024) + `"}`

	recorder := serve(t, handler, denyAuth, http.MethodPost, "/api/v1/webhooks/receipts/sms",
		map[string]string{"X-Receipt-Signature": signBody(testSecret, body)}, body)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(store.updated) != 0 {
		t.Fatal("oversized body applied receipt")
	}
}

func TestParseGenericReceipt(t *testing.T) {
	payload, err := ParseGenericReceipt([]byte(`{"providerMessageId":"provider-9","delivered":false,"errorCode":"ERR_X"}`))
	if err != nil {
		t.Fatalf("parse error = %v", err)
	}
	if payload.ProviderMessageID != "provider-9" || payload.Delivered || payload.ErrorCode != "ERR_X" {
		t.Fatalf("payload = %#v", payload)
	}
	if _, err := ParseGenericReceipt([]byte(`{"delivered":true}`)); err == nil {
		t.Fatal("missing providerMessageId parsed without error")
	}
	if _, err := ParseGenericReceipt([]byte(`{bad`)); err == nil {
		t.Fatal("malformed json parsed without error")
	}
}

type outboxCall struct {
	address string
	limit   int
}

type fakeOutboxStore struct {
	calls    []outboxCall
	messages []DevOutboxMessage
	err      error
}

func (s *fakeOutboxStore) LatestByAddress(_ context.Context, address string, limit int) ([]DevOutboxMessage, error) {
	s.calls = append(s.calls, outboxCall{address, limit})
	return s.messages, s.err
}

func serveOutbox(t *testing.T, store *fakeOutboxStore, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	recorder := httptest.NewRecorder()
	NewDevOutboxHandler(store).ServeHTTP(recorder, req)
	return recorder
}

func TestDevOutboxReturnsLatestFiveShape(t *testing.T) {
	messages := make([]DevOutboxMessage, 0, 5)
	for index := range 5 {
		messages = append(messages, DevOutboxMessage{
			Address:   "+8613800000000",
			Channel:   "sms",
			TodoID:    "todo-1",
			Body:      "记得提交周报",
			CreatedAt: testNow.Add(-time.Duration(index) * time.Minute),
		})
	}
	store := &fakeOutboxStore{messages: messages}

	recorder := serveOutbox(t, store, "/api/v1/dev/reminder-outbox?address=%2B8613800000000")
	if recorder.Code != http.StatusOK {
		t.Fatalf("outbox status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Messages []struct {
			Address   string `json:"address"`
			Channel   string `json:"channel"`
			TodoID    string `json:"todoId"`
			Body      string `json:"body"`
			CreatedAt string `json:"createdAt"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if len(body.Messages) != 5 {
		t.Fatalf("messages = %d, want 5", len(body.Messages))
	}
	first := body.Messages[0]
	if first.Address != "+8613800000000" || first.Channel != "sms" || first.TodoID != "todo-1" || first.Body != "记得提交周报" {
		t.Fatalf("message = %#v", first)
	}
	if createdAt, err := time.Parse(time.RFC3339, first.CreatedAt); err != nil || !createdAt.Equal(testNow) {
		t.Fatalf("createdAt = %q, want %v", first.CreatedAt, testNow)
	}
	if len(store.calls) != 1 || store.calls[0].address != "+8613800000000" || store.calls[0].limit != 5 {
		t.Fatalf("store calls = %#v, want one call with limit 5", store.calls)
	}
}

func TestDevOutboxMissingAddressRejected(t *testing.T) {
	store := &fakeOutboxStore{}
	recorder := serveOutbox(t, store, "/api/v1/dev/reminder-outbox")
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", recorder.Code)
	}
	if envelope := decodeEnvelope(t, recorder); envelope["code"] != "validation_error" {
		t.Fatalf("envelope = %#v, want validation_error", envelope)
	}
	if len(store.calls) != 0 {
		t.Fatal("store called despite missing address")
	}
}

func TestDevOutboxEmptyResultReturnsEmptyArray(t *testing.T) {
	store := &fakeOutboxStore{}
	recorder := serveOutbox(t, store, "/api/v1/dev/reminder-outbox?address=nobody")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"messages":[]`) {
		t.Fatalf("body = %s, want messages array []", recorder.Body.String())
	}
}

func TestDevOutboxStoreErrorMapsTo500(t *testing.T) {
	store := &fakeOutboxStore{err: io.ErrUnexpectedEOF}
	recorder := serveOutbox(t, store, "/api/v1/dev/reminder-outbox?address=%2B8613800000000")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if envelope := decodeEnvelope(t, recorder); envelope["code"] != "internal_error" {
		t.Fatalf("envelope = %#v, want internal_error", envelope)
	}
}

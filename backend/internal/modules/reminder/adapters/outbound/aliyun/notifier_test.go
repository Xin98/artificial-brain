package aliyun

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
)

var _ ports.SmsNotifier = (*Notifier)(nil)

const (
	testAccessKeyID     = "test-access-key-id"
	testAccessKeySecret = "test-access-key-secret"
	testSignName        = "人工大脑"
	testTemplateCode    = "SMS_123456"
)

var testNow = time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)

func smsMessage() dto.ReminderMessage {
	return dto.ReminderMessage{
		To:             "13800138000",
		TodoID:         "todo-123",
		Title:          "提交周报",
		ScheduledAtUTC: testNow,
	}
}

// capturedServer records every form the notifier POSTs and answers with a
// scripted status + JSON body.
type capturedServer struct {
	mu       sync.Mutex
	requests []*http.Request
	forms    []url.Values
	status   int
	body     string
}

func startAliyunServer(t *testing.T, status int, body string) (*capturedServer, *httptest.Server) {
	t.Helper()
	captured := &capturedServer{status: status, body: body}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		clone := r.Clone(r.Context())
		captured.mu.Lock()
		captured.requests = append(captured.requests, clone)
		captured.forms = append(captured.forms, r.PostForm)
		captured.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(captured.status)
		io.WriteString(w, captured.body)
	}))
	t.Cleanup(server.Close)
	return captured, server
}

func (c *capturedServer) form(t *testing.T, index int) url.Values {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if index >= len(c.forms) {
		t.Fatalf("captured %d requests, want at least %d", len(c.forms), index+1)
	}
	return c.forms[index]
}

func (c *capturedServer) request(t *testing.T, index int) *http.Request {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if index >= len(c.requests) {
		t.Fatalf("captured %d requests, want at least %d", len(c.requests), index+1)
	}
	return c.requests[index]
}

// newTestNotifier wires a notifier with deterministic nonce and clock.
func newTestNotifier(cfg Config) *Notifier {
	notifier := New(cfg)
	notifier.nonce = func() string { return "fixed-nonce-123" }
	notifier.now = func() time.Time { return testNow }
	return notifier
}

func testConfig(endpoint string) Config {
	return Config{
		Endpoint:        endpoint,
		AccessKeyID:     testAccessKeyID,
		AccessKeySecret: testAccessKeySecret,
		SignName:        testSignName,
		TemplateCode:    testTemplateCode,
		Timeout:         5 * time.Second,
	}
}

// goldenPercentEncode is an independent copy of Aliyun's RFC 3986 encoding so
// the signature test does not share code with the implementation under test.
func goldenPercentEncode(s string) string {
	escaped := url.QueryEscape(s)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	escaped = strings.ReplaceAll(escaped, "*", "%2A")
	escaped = strings.ReplaceAll(escaped, "%7E", "~")
	return escaped
}

// goldenSignature recomputes the Aliyun RPC v1 signature from the captured
// form using only the documented algorithm.
func goldenSignature(form url.Values, secret string) string {
	keys := make([]string, 0, len(form))
	for key := range form {
		if key == "Signature" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, goldenPercentEncode(key)+"="+goldenPercentEncode(form.Get(key)))
	}
	stringToSign := "POST&%2F&" + goldenPercentEncode(strings.Join(pairs, "&"))
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestSendSignsRequestDeterministically(t *testing.T) {
	captured, server := startAliyunServer(t, http.StatusOK, `{"RequestId":"req-1","Code":"OK","Message":"OK","BizId":"biz-0001"}`)
	notifier := newTestNotifier(testConfig(server.URL))

	if _, err := notifier.Send(context.Background(), smsMessage()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if _, err := notifier.Send(context.Background(), smsMessage()); err != nil {
		t.Fatalf("Send() second call error = %v", err)
	}

	first := captured.form(t, 0)
	second := captured.form(t, 1)

	// Fixed nonce and clock must yield byte-identical signed requests.
	if first.Encode() != second.Encode() {
		t.Fatalf("signed forms differ across identical sends:\n%s\n%s", first.Encode(), second.Encode())
	}

	signature := first.Get("Signature")
	if signature == "" {
		t.Fatal("Signature parameter missing from the request")
	}
	if want := goldenSignature(first, testAccessKeySecret); signature != want {
		t.Fatalf("Signature = %q, want golden %q", signature, want)
	}

	wantParams := map[string]string{
		"Action":           "SendSms",
		"Version":          "2017-05-25",
		"Format":           "JSON",
		"AccessKeyId":      testAccessKeyID,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":   "fixed-nonce-123",
		"Timestamp":        "2026-08-20T08:00:00Z",
		"PhoneNumbers":     "13800138000",
		"SignName":         testSignName,
		"TemplateCode":     testTemplateCode,
	}
	for key, want := range wantParams {
		if got := first.Get(key); got != want {
			t.Fatalf("param %s = %q, want %q", key, got, want)
		}
	}
	if first.Get("TemplateParam") == "" {
		t.Fatal("TemplateParam parameter missing from the request")
	}

	request := captured.request(t, 0)
	if request.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", request.Method)
	}
	if contentType := request.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
		t.Fatalf("Content-Type = %q, want form-urlencoded", contentType)
	}
}

func TestSendOKReturnsBizID(t *testing.T) {
	_, server := startAliyunServer(t, http.StatusOK, `{"RequestId":"req-1","Code":"OK","Message":"OK","BizId":"biz-0001"}`)
	notifier := newTestNotifier(testConfig(server.URL))

	result, err := notifier.Send(context.Background(), smsMessage())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if result.ProviderMessageID != "biz-0001" {
		t.Fatalf("ProviderMessageID = %q, want biz-0001", result.ProviderMessageID)
	}
}

func TestSendThrottledIsTransient(t *testing.T) {
	_, server := startAliyunServer(t, http.StatusOK, `{"RequestId":"req-2","Code":"isThrottled","Message":"Too frequent."}`)
	notifier := newTestNotifier(testConfig(server.URL))

	_, err := notifier.Send(context.Background(), smsMessage())
	if err == nil {
		t.Fatal("Send() error = nil, want throttled failure")
	}
	if errors.Is(err, ports.ErrPermanent) {
		t.Fatalf("Send() error = %v, want transient throttle", err)
	}
}

func TestSendIllegalNumberIsPermanentWithCode(t *testing.T) {
	_, server := startAliyunServer(t, http.StatusOK, `{"RequestId":"req-3","Code":"isv.MOBILE_NUMBER_ILLEGAL","Message":"手机号格式错误"}`)
	notifier := newTestNotifier(testConfig(server.URL))

	_, err := notifier.Send(context.Background(), smsMessage())
	if err == nil {
		t.Fatal("Send() error = nil, want permanent refusal")
	}
	if !errors.Is(err, ports.ErrPermanent) {
		t.Fatalf("Send() error = %v, want errors.Is(err, ports.ErrPermanent)", err)
	}
	var permanent *ports.PermanentError
	if !errors.As(err, &permanent) || permanent.Code != "isv.MOBILE_NUMBER_ILLEGAL" {
		t.Fatalf("Send() error = %v, want PermanentError code isv.MOBILE_NUMBER_ILLEGAL", err)
	}
}

func TestSendHttp500IsTransient(t *testing.T) {
	_, server := startAliyunServer(t, http.StatusInternalServerError, `{"RequestId":"req-4","Code":"InternalError","Message":"boom"}`)
	notifier := newTestNotifier(testConfig(server.URL))

	_, err := notifier.Send(context.Background(), smsMessage())
	if err == nil {
		t.Fatal("Send() error = nil, want HTTP 500 failure")
	}
	if errors.Is(err, ports.ErrPermanent) {
		t.Fatalf("Send() error = %v, want transient HTTP 500", err)
	}
}

func TestSendMalformedResponseIsTransient(t *testing.T) {
	_, server := startAliyunServer(t, http.StatusOK, `not-json`)
	notifier := newTestNotifier(testConfig(server.URL))

	_, err := notifier.Send(context.Background(), smsMessage())
	if err == nil {
		t.Fatal("Send() error = nil, want malformed-body failure")
	}
	if errors.Is(err, ports.ErrPermanent) {
		t.Fatalf("Send() error = %v, want transient parse failure", err)
	}
}

func TestSendEmptyCodeIsTransient(t *testing.T) {
	// A 200 reply without a verdict code is malformed and must be retried,
	// not refused as permanent with an empty code.
	_, server := startAliyunServer(t, http.StatusOK, `{}`)
	notifier := newTestNotifier(testConfig(server.URL))

	_, err := notifier.Send(context.Background(), smsMessage())
	if err == nil {
		t.Fatal("Send() error = nil, want missing-verdict failure")
	}
	if errors.Is(err, ports.ErrPermanent) {
		t.Fatalf("Send() error = %v, want transient missing-verdict failure", err)
	}
}

func TestSendOKWithoutBizIDIsTransient(t *testing.T) {
	// An OK without a BizId is no identifiably accepted message; retry it.
	_, server := startAliyunServer(t, http.StatusOK, `{"RequestId":"req-5","Code":"OK","Message":"OK","BizId":""}`)
	notifier := newTestNotifier(testConfig(server.URL))

	_, err := notifier.Send(context.Background(), smsMessage())
	if err == nil {
		t.Fatal("Send() error = nil, want missing-BizId failure")
	}
	if errors.Is(err, ports.ErrPermanent) {
		t.Fatalf("Send() error = %v, want transient missing-BizId failure", err)
	}
}

func TestNewDefaultsClientTimeout(t *testing.T) {
	notifier := New(Config{})
	if notifier.client == nil {
		t.Fatal("client = nil, want a timeout-bounded HTTP client")
	}
	if got, want := notifier.client.Timeout, fallbackTimeout; got != want {
		t.Fatalf("client timeout = %v, want the %v fallback", got, want)
	}
}

func TestSendContextTimeoutIsTransient(t *testing.T) {
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(stalled.Close)

	cfg := testConfig(stalled.URL)
	cfg.Timeout = 5 * time.Second
	notifier := newTestNotifier(cfg)

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(50*time.Millisecond))
	defer cancel()
	start := time.Now()
	_, err := notifier.Send(ctx, smsMessage())
	if err == nil {
		t.Fatal("Send() error = nil, want context deadline failure")
	}
	if errors.Is(err, ports.ErrPermanent) {
		t.Fatalf("Send() error = %v, want transient timeout", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Send() took %v, want the ~50ms context deadline honored", elapsed)
	}
}

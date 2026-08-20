// Package aliyun is the ITER-0003 real SMS notifier behind ports.SmsNotifier
// and its delivery-receipt parser: it calls Aliyun's RPC SendSms API
// (version 2017-05-25) with an HMAC-SHA1 signed request. Throttling, HTTP
// 5xx, and transport failures are transient; every other refusal code is
// permanent.
package aliyun

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
)

const (
	sendSmsAction = "SendSms"
	apiVersion    = "2017-05-25"

	// codeOK is Aliyun's acceptance verdict; codeThrottled asks the caller to
	// slow down and is retried by the queue.
	codeOK        = "OK"
	codeThrottled = "isThrottled"

	timestampFormat = "2006-01-02T15:04:05Z"
)

// Config carries the Aliyun SMS credentials and template.
type Config struct {
	Endpoint        string
	AccessKeyID     string
	AccessKeySecret string
	SignName        string
	TemplateCode    string
	Timeout         time.Duration
}

// Notifier submits reminder SMS through Aliyun's SendSms RPC.
type Notifier struct {
	cfg Config
	// do, nonce, and now are injectable so tests run against httptest with a
	// deterministic signature; the defaults are a timeout-bounded HTTP client,
	// a random nonce, and the wall clock.
	do    func(*http.Request) (*http.Response, error)
	nonce func() string
	now   func() time.Time
}

var _ ports.SmsNotifier = (*Notifier)(nil)

// New returns an Aliyun SMS notifier for cfg.
func New(cfg Config) *Notifier {
	client := &http.Client{Timeout: cfg.Timeout}
	return &Notifier{
		cfg:   cfg,
		do:    client.Do,
		nonce: randomNonce,
		now:   time.Now,
	}
}

// sendSmsResponse is the subset of the SendSms reply the notifier acts on.
type sendSmsResponse struct {
	RequestID string `json:"RequestId"`
	Code      string `json:"Code"`
	Message   string `json:"Message"`
	BizID     string `json:"BizId"`
}

// Send submits one reminder SMS. OK returns the provider BizId as the
// message id; throttling, HTTP 5xx, timeouts, and malformed replies are
// transient; every other refusal code is a *ports.PermanentError carrying
// Aliyun's code.
func (n *Notifier) Send(ctx context.Context, message dto.ReminderMessage) (dto.SendResult, error) {
	params := n.signedParams(message)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.Endpoint, strings.NewReader(encode(params)))
	if err != nil {
		return dto.SendResult{}, fmt.Errorf("aliyun: build SendSms request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := n.do(request)
	if err != nil {
		return dto.SendResult{}, fmt.Errorf("aliyun: SendSms: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return dto.SendResult{}, fmt.Errorf("aliyun: read SendSms reply: %w", err)
	}
	if response.StatusCode >= 500 && response.StatusCode <= 599 {
		return dto.SendResult{}, fmt.Errorf("aliyun: SendSms: HTTP %d: %s", response.StatusCode, truncate(body))
	}
	var payload sendSmsResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return dto.SendResult{}, fmt.Errorf("aliyun: parse SendSms reply: %w", err)
	}
	switch payload.Code {
	case codeOK:
		return dto.SendResult{ProviderMessageID: payload.BizID}, nil
	case codeThrottled:
		return dto.SendResult{}, fmt.Errorf("aliyun: SendSms throttled: %s", payload.Message)
	default:
		return dto.SendResult{}, &ports.PermanentError{
			Code:  payload.Code,
			Cause: fmt.Errorf("aliyun: SendSms refused: %s: %s", payload.Code, payload.Message),
		}
	}
}

// signedParams builds the full SendSms parameter set including the Aliyun
// RPC v1 signature.
func (n *Notifier) signedParams(message dto.ReminderMessage) url.Values {
	params := url.Values{}
	params.Set("AccessKeyId", n.cfg.AccessKeyID)
	params.Set("Action", sendSmsAction)
	params.Set("Format", "JSON")
	params.Set("PhoneNumbers", message.To)
	params.Set("SignName", n.cfg.SignName)
	params.Set("SignatureMethod", "HMAC-SHA1")
	params.Set("SignatureNonce", n.nonce())
	params.Set("SignatureVersion", "1.0")
	params.Set("TemplateCode", n.cfg.TemplateCode)
	params.Set("TemplateParam", templateParam(message))
	params.Set("Timestamp", n.now().UTC().Format(timestampFormat))
	params.Set("Version", apiVersion)
	params.Set("Signature", sign(params, n.cfg.AccessKeySecret))
	return params
}

// sign computes base64(hmac-sha1(secret + "&", "POST&%2F&" +
// percentEncode(canonicalQuery))) over the sorted, percent-encoded
// parameters, per Aliyun's RPC signing rules.
func sign(params url.Values, accessKeySecret string) string {
	stringToSign := "POST&%2F&" + percentEncode(canonicalQuery(params))
	mac := hmac.New(sha1.New, []byte(accessKeySecret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// canonicalQuery sorts the parameters (excluding Signature itself) into
// percentEncode(key)=percentEncode(value) pairs joined by "&".
func canonicalQuery(params url.Values) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		if key == "Signature" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, percentEncode(key)+"="+percentEncode(params.Get(key)))
	}
	return strings.Join(pairs, "&")
}

// encode renders the signed form body with Aliyun's RFC 3986 encoding —
// url.Values.Encode's "+" for spaces would corrupt the signature.
func encode(params url.Values) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, percentEncode(key)+"="+percentEncode(params.Get(key)))
	}
	return strings.Join(pairs, "&")
}

// percentEncode applies Aliyun's RFC 3986 flavor on top of url.QueryEscape:
// space must be %20 (not "+"), "*" must be %2A, and "~" stays unescaped.
func percentEncode(s string) string {
	escaped := url.QueryEscape(s)
	escaped = strings.ReplaceAll(escaped, "+", "%20")
	escaped = strings.ReplaceAll(escaped, "*", "%2A")
	escaped = strings.ReplaceAll(escaped, "%7E", "~")
	return escaped
}

// templateParam renders the JSON template variables: the reminder title and
// its scheduled instant.
func templateParam(message dto.ReminderMessage) string {
	payload, err := json.Marshal(map[string]string{
		"time":  message.ScheduledAtUTC.UTC().Format("2006-01-02 15:04"),
		"title": message.Title,
	})
	if err != nil {
		return "{}"
	}
	return string(payload)
}

// randomNonce returns a fresh random signature nonce.
func randomNonce() string {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(random)
}

// truncate keeps error messages from ballooning on HTML error pages.
func truncate(body []byte) string {
	const limit = 256
	if len(body) > limit {
		return string(body[:limit]) + "…"
	}
	return string(body)
}

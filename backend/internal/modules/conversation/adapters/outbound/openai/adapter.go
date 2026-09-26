// Package openai is the config-gated OpenAI-compatible ModelPort. It posts
// the turn to {base}/chat/completions and returns the assistant content raw;
// all schema validation stays in the conversation application layer.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
)

var (
	// ErrInvalidConfig marks incomplete adapter configuration.
	ErrInvalidConfig = errors.New("openai: adapter configuration is incomplete")
	// ErrRequestFailed marks non-2xx model responses.
	ErrRequestFailed = errors.New("openai: model request failed")
	// ErrMalformedResponse marks bodies without a usable assistant content.
	ErrMalformedResponse = errors.New("openai: malformed model response")
)

// Adapter implements ports.ModelPort against an OpenAI-compatible endpoint.
type Adapter struct {
	cfg    Config
	client *http.Client
}

var _ ports.ModelPort = (*Adapter)(nil)

// New validates the configuration and builds the adapter.
func New(cfg Config) (*Adapter, error) {
	if cfg.BaseURL == "" || cfg.APIKey == "" || cfg.ModelName == "" || cfg.Timeout <= 0 {
		return nil, ErrInvalidConfig
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Adapter{cfg: cfg, client: &http.Client{}}, nil
}

// Propose sends the turn to the model and returns its raw content.
func (a *Adapter) Propose(ctx context.Context, in ports.MessageInput) (json.RawMessage, error) {
	operationCtx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()

	payload, err := json.Marshal(chatRequest{
		Model: a.cfg.ModelName,
		Messages: []chatMessage{
			{Role: "system", Content: buildSystemPrompt(in, a.cfg.Now())},
			{Role: "user", Content: in.Text},
		},
		ResponseFormat: chatResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(a.cfg.BaseURL, "/") + "/chat/completions"
	for attempt := 0; attempt < 2; attempt++ {
		request, err := http.NewRequestWithContext(operationCtx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
		request.Header.Set("Content-Type", "application/json")

		response, err := a.client.Do(request)
		if err != nil {
			if attempt == 0 && retryableTimeout(operationCtx, err) {
				continue
			}
			return nil, err
		}
		if response.StatusCode < 200 || response.StatusCode > 299 {
			status := response.StatusCode
			_ = response.Body.Close()
			if attempt == 0 && operationCtx.Err() == nil &&
				(status == http.StatusTooManyRequests || status >= 500 && status <= 599) {
				continue
			}
			return nil, fmt.Errorf("%w: status %d", ErrRequestFailed, status)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			if attempt == 0 && retryableTimeout(operationCtx, err) {
				continue
			}
			return nil, err
		}
		var decoded chatResponse
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, ErrMalformedResponse
		}
		if len(decoded.Choices) == 0 || decoded.Choices[0].Message.Content == "" {
			return nil, ErrMalformedResponse
		}
		return json.RawMessage(decoded.Choices[0].Message.Content), nil
	}
	return nil, ErrRequestFailed
}

func retryableTimeout(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}

func buildSystemPrompt(in ports.MessageInput, now time.Time) string {
	currentTime := now.UTC()
	if location, err := time.LoadLocation(in.Timezone); err == nil {
		currentTime = now.In(location)
	}
	return fmt.Sprintf(
		`你是待办意图解析器。只返回一个 JSON 对象，不要返回 Markdown、代码围栏或解释。唯一允许的 JSON Schema 是：{"type":"object","additionalProperties":false,"required":["schemaVersion","intent","arguments","confidence","missingFields"],"properties":{"schemaVersion":{"const":"1"},"intent":{"enum":["todo.create","todo.delete","todo.list","unknown"]},"arguments":{"type":"object","additionalProperties":false,"properties":{"title":{"type":"string","minLength":1,"maxLength":200},"description":{"type":"string"},"dueAtUtc":{"type":"string","format":"date-time"},"timezoneAtInput":{"type":"string"},"keyword":{"type":"string","maxLength":100},"status":{"enum":["pending","completed"]},"dueFrom":{"type":"string","format":"date-time"},"dueTo":{"type":"string","format":"date-time"},"noDue":{"type":"boolean"}}},"confidence":{"type":"number","minimum":0,"maximum":1},"missingFields":{"type":"array","items":{"type":"string"}}}}。todo.create 的 arguments 只能包含 title、description、dueAtUtc、timezoneAtInput；todo.delete 必须且只能包含 keyword；todo.list 只能包含 keyword、status、dueFrom、dueTo、noDue；unknown 的 arguments 必须为空对象。信息不足时列入 missingFields，禁止编造。当前时间：%s。用户时区：%s。`,
		currentTime.Format(time.RFC3339), in.Timezone,
	)
}

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
	"net/http"
	"strings"

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
	return &Adapter{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}}, nil
}

// Propose sends the turn to the model and returns its raw content.
func (a *Adapter) Propose(ctx context.Context, in ports.MessageInput) (json.RawMessage, error) {
	payload, err := json.Marshal(chatRequest{
		Model:    a.cfg.ModelName,
		Messages: []chatMessage{{Role: "user", Content: buildPrompt(in)}},
	})
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(a.cfg.BaseURL, "/") + "/chat/completions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := a.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("%w: status %d", ErrRequestFailed, response.StatusCode)
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

func buildPrompt(in ports.MessageInput) string {
	return fmt.Sprintf(
		"把下面的用户消息转换为严格的 JSON 意图提案：schemaVersion 固定为 \"1\"，intent 只能是 todo.create、todo.delete、todo.list 或 unknown，信息不足时通过 missingFields 澄清，不要编造。用户时区：%s。用户消息：%s",
		in.Timezone, in.Text,
	)
}

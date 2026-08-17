package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestLoggerIncludesServiceFieldsAndCorrelationID(t *testing.T) {
	var out bytes.Buffer
	logger := NewLogger(&out, "api", "abc123")
	ctx := WithCorrelationID(context.Background(), "req-123")
	logger.InfoContext(ctx, "started")

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"service": "api", "version": "abc123", "msg": "started", "correlation_id": "req-123",
	} {
		if got[key] != want {
			t.Fatalf("%s = %#v, want %q", key, got[key], want)
		}
	}
	if _, ok := got["time"].(string); !ok {
		t.Fatalf("time = %#v, want string", got["time"])
	}
	if got["level"] != "INFO" {
		t.Fatalf("level = %#v, want INFO", got["level"])
	}
}

func TestLoggerOmitsEmptyCorrelationID(t *testing.T) {
	var out bytes.Buffer
	NewLogger(&out, "api", "abc123").InfoContext(context.Background(), "started")

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["correlation_id"]; ok {
		t.Fatalf("unexpected correlation ID: %#v", got["correlation_id"])
	}
}

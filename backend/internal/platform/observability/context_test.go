package observability

import (
	"context"
	"testing"
)

func TestCorrelationIDRoundTrip(t *testing.T) {
	ctx := WithCorrelationID(context.Background(), "req-123")
	if got := CorrelationID(ctx); got != "req-123" {
		t.Fatalf("correlation ID = %q", got)
	}
}

func TestCorrelationIDIsEmptyWhenNotPresent(t *testing.T) {
	if got := CorrelationID(context.Background()); got != "" {
		t.Fatalf("correlation ID = %q", got)
	}
}

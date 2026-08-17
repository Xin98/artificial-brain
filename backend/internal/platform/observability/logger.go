package observability

import (
	"context"
	"io"
	"log/slog"
)

func NewLogger(w io.Writer, service, version string) *slog.Logger {
	handler := correlationHandler{Handler: slog.NewJSONHandler(w, nil)}
	return slog.New(handler).With("service", service, "version", version)
}

type correlationHandler struct {
	slog.Handler
}

func (h correlationHandler) Handle(ctx context.Context, record slog.Record) error {
	if correlationID := CorrelationID(ctx); correlationID != "" {
		record.AddAttrs(slog.String("correlation_id", correlationID))
	}
	return h.Handler.Handle(ctx, record)
}

func (h correlationHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return correlationHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h correlationHandler) WithGroup(name string) slog.Handler {
	return correlationHandler{Handler: h.Handler.WithGroup(name)}
}

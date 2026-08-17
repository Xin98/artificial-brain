package workerstatus

import (
	"context"
	"errors"
	"time"
)

const cleanupTimeout = 2 * time.Second

type Recorder interface {
	Record(context.Context, Instance) error
	Remove(context.Context, string) error
}

type TickSource interface {
	C() <-chan time.Time
	Stop()
}

type Heartbeat struct {
	recorder Recorder
	instance Instance
	ticks    TickSource
}

func NewHeartbeat(recorder Recorder, instance Instance, ticks TickSource) *Heartbeat {
	return &Heartbeat{recorder: recorder, instance: instance, ticks: ticks}
}

func (h *Heartbeat) Run(ctx context.Context) error {
	defer h.ticks.Stop()

	if err := h.record(ctx); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return h.cleanup()
		case <-h.ticks.C():
			if ctx.Err() != nil {
				return h.cleanup()
			}
			if err := h.record(ctx); err != nil {
				return err
			}
		}
	}
}

func (h *Heartbeat) record(ctx context.Context) error {
	err := h.recorder.Record(ctx, h.instance)
	if err != nil && ctx.Err() != nil {
		_ = h.cleanup()
	}
	return err
}

func (h *Heartbeat) cleanup() error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	return h.recorder.Remove(cleanupCtx, h.instance.ID)
}

type timeTickSource struct {
	ticker *time.Ticker
}

func NewTimeTickSource(interval time.Duration) (TickSource, error) {
	if interval <= 0 {
		return nil, errors.New("heartbeat interval must be positive")
	}
	return &timeTickSource{ticker: time.NewTicker(interval)}, nil
}

func (s *timeTickSource) C() <-chan time.Time { return s.ticker.C }

func (s *timeTickSource) Stop() { s.ticker.Stop() }

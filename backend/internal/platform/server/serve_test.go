package server

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeShutsDownAfterContextCancellation(t *testing.T) {
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := &http.Server{
		Addr: "127.0.0.1:0",
		BaseContext: func(net.Listener) context.Context {
			close(started)
			return context.Background()
		},
	}
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, time.Second) }()

	select {
	case <-started:
		cancel()
	case err := <-done:
		t.Fatalf("Serve returned before startup: %v", err)
	case <-time.After(time.Second):
		t.Fatal("server did not start")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not exit after cancellation")
	}
}

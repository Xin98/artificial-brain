package server

import (
	"context"
	"errors"
	"net/http"
	"time"
)

func Serve(ctx context.Context, srv *http.Server, shutdownTimeout time.Duration) error {
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.ListenAndServe() }()

	select {
	case err := <-serveDone:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		err := <-serveDone
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

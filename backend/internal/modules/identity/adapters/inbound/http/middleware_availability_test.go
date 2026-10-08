package http

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddlewareDistinguishesOutageFromInvalidSession(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
	}{{errors.New("database unavailable"), 503}, {context.DeadlineExceeded, 503}, {domain.ErrSessionNotFound, 401}, {domain.ErrSessionInactive, 401}} {
		middleware := NewAuthMiddleware(func(context.Context, string) (dto.Principal, error) { return dto.Principal{}, c.err })
		h := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("must not enter private route") }))
		r := httptest.NewRequest("GET", "/api/v1/auth/session", nil)
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "cookie"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.status {
			t.Fatal(c.err, w.Code)
		}
	}
}

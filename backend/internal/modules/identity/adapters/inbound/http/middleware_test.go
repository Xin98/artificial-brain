package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
)

func TestAuthMiddlewareRejectsMissingAndInvalidCookie(t *testing.T) {
	auth := NewAuthMiddleware(func(ctx context.Context, token string) (dto.Principal, error) {
		return dto.Principal{}, http.ErrNoCookie
	})
	handler := auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not run")
	}))

	for _, name := range []string{"no cookie", "empty cookie"} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
			if name == "empty cookie" {
				req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: ""})
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rr.Code)
			}
		})
	}
}

func TestAuthMiddlewareInjectsPrincipal(t *testing.T) {
	want := dto.Principal{UserID: "u1", WorkspaceID: "w1", SessionID: "s1"}
	auth := NewAuthMiddleware(func(ctx context.Context, token string) (dto.Principal, error) {
		if token != "valid-token" {
			return dto.Principal{}, http.ErrNoCookie
		}
		return want, nil
	})

	var got dto.Principal
	handler := auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = dto.PrincipalFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "valid-token"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got != want {
		t.Fatalf("principal = %#v, want %#v", got, want)
	}
}

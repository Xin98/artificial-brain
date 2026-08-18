package http

import (
	"context"
	"net/http"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
)

// Authenticator resolves a bearer token to a principal.
type Authenticator func(ctx context.Context, token string) (dto.Principal, error)

// NewAuthMiddleware returns middleware that requires a valid ab_session cookie
// and places the resolved principal on the request context. It lives in
// Identity (not platform) because platform may not import business modules.
func NewAuthMiddleware(authenticator Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(SessionCookieName)
			if err != nil || cookie.Value == "" {
				writeUnauthenticated(w, r)
				return
			}
			principal, err := authenticator(r.Context(), cookie.Value)
			if err != nil {
				writeUnauthenticated(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(dto.WithPrincipal(r.Context(), principal)))
		})
	}
}

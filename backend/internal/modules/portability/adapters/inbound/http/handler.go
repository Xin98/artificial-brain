// Package http serves the portability routes: the export bundle download and
// the two-phase import — upload with preview, get, confirm. The principal
// arrives on the context via Identity's session middleware; this package
// never touches cookies or tokens.
package http

import (
	"net/http"

	identitydto "github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/query"
)

// Handler serves the portability HTTP routes. MaxBundleBytes caps the
// uploaded bundle size; the cap is enforced defensively on both the request
// body and the multipart part.
type Handler struct {
	Export         *command.ExportBundleHandler
	Upload         *command.UploadImportHandler
	Confirm        *command.ConfirmImportHandler
	Get            *query.GetImportQuery
	MaxBundleBytes int64
}

// RegisterRoutes registers the portability routes on mux, all wrapped with
// the auth middleware.
func RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler, h *Handler) {
	mux.Handle("POST /api/v1/portability/export", auth(http.HandlerFunc(h.export)))
	mux.Handle("POST /api/v1/portability/imports", auth(http.HandlerFunc(h.upload)))
	mux.Handle("GET /api/v1/portability/imports/{importId}", auth(http.HandlerFunc(h.get)))
	mux.Handle("POST /api/v1/portability/imports/{importId}/confirm", auth(http.HandlerFunc(h.confirm)))
}

// principalFrom maps the identity principal the auth middleware injected on
// the context into the portability-local shape. The auth middleware always
// wraps these routes; the guard answers 401 anyway if it ever did not run.
func principalFrom(w http.ResponseWriter, r *http.Request) (ports.Principal, bool) {
	principal, ok := identitydto.PrincipalFromContext(r.Context())
	if !ok {
		writeUnauthenticated(w, r)
		return ports.Principal{}, false
	}
	return ports.Principal{UserID: principal.UserID, WorkspaceID: principal.WorkspaceID}, true
}

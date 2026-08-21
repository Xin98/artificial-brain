package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

// Narrow application seams consumed by the HTTP handlers. The concrete
// command/query handlers satisfy these interfaces.
type (
	loginRequester interface {
		Handle(ctx context.Context, phone string) error
	}
	loginVerifier interface {
		Handle(ctx context.Context, phone, code string) (dto.VerifyLoginChallengeResult, error)
	}
	logoutHandler interface {
		Handle(ctx context.Context, sessionID string) error
	}
	channelAdder interface {
		Handle(ctx context.Context, p dto.Principal, kind, address string) (dto.ContactChannelView, error)
	}
	channelVerifier interface {
		Handle(ctx context.Context, p dto.Principal, channelID, code string) error
	}
	channelEnabledSetter interface {
		Handle(ctx context.Context, p dto.Principal, channelID string, enabled bool) (dto.ContactChannelView, error)
	}
	channelsLister interface {
		GetContactChannels(ctx context.Context, p dto.Principal) ([]dto.ContactChannelView, error)
	}
)

// Handler serves the identity HTTP routes.
type Handler struct {
	RequestLoginChallenge loginRequester
	VerifyLoginChallenge  loginVerifier
	Logout                logoutHandler
	AddChannel            channelAdder
	VerifyChannel         channelVerifier
	SetChannelEnabled     channelEnabledSetter
	Channels              channelsLister
	SessionTTL            time.Duration
}

// RegisterRoutes registers the identity routes on mux. Protected routes are
// wrapped with the auth middleware; login request/verify are public.
func RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler, h *Handler) {
	mux.HandleFunc("POST /api/v1/auth/login/request", h.requestLogin)
	mux.HandleFunc("POST /api/v1/auth/login/verify", h.verifyLogin)
	mux.Handle("POST /api/v1/auth/logout", auth(http.HandlerFunc(h.logout)))
	mux.Handle("GET /api/v1/auth/session", auth(http.HandlerFunc(h.session)))
	mux.Handle("GET /api/v1/settings/contact-channels", auth(http.HandlerFunc(h.listChannels)))
	mux.Handle("POST /api/v1/settings/contact-channels", auth(http.HandlerFunc(h.addChannel)))
	mux.Handle("POST /api/v1/settings/contact-channels/{channelId}/verify", auth(http.HandlerFunc(h.verifyChannel)))
	mux.Handle("PATCH /api/v1/settings/contact-channels/{channelId}", auth(http.HandlerFunc(h.setChannelEnabled)))
}

func (h *Handler) requestLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.RequestLoginChallenge.Handle(r.Context(), body.Phone); err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidPhone):
			writeValidationError(w, r)
		case errors.Is(err, domain.ErrRateLimited):
			writeError(w, r, http.StatusTooManyRequests, "rate_limited", "too many requests")
		case errors.Is(err, domain.ErrRegistrationClosed):
			writeError(w, r, http.StatusForbidden, "registration_closed", "registration is closed")
		default:
			writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{})
}

func (h *Handler) verifyLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	result, err := h.VerifyLoginChallenge.Handle(r.Context(), body.Phone, body.Code)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidPhone):
			writeValidationError(w, r)
		case errors.Is(err, domain.ErrRegistrationClosed):
			writeError(w, r, http.StatusForbidden, "registration_closed", "registration is closed")
		default:
			writeError(w, r, http.StatusUnauthorized, "unauthenticated", "login failed")
		}
		return
	}
	h.setSessionCookie(w, result.Token)
	writeJSON(w, http.StatusOK, map[string]string{
		"userId":      result.Principal.UserID,
		"workspaceId": result.Principal.WorkspaceID,
		"expiresAt":   result.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.SessionTTL.Seconds()),
	})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	principal, ok := dto.PrincipalFromContext(r.Context())
	if !ok {
		writeUnauthenticated(w, r)
		return
	}
	_ = h.Logout.Handle(r.Context(), principal.SessionID)
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{})
}

func (h *Handler) session(w http.ResponseWriter, r *http.Request) {
	principal, ok := dto.PrincipalFromContext(r.Context())
	if !ok {
		writeUnauthenticated(w, r)
		return
	}
	writeJSON(w, http.StatusOK, dto.SessionView{
		UserID:      principal.UserID,
		WorkspaceID: principal.WorkspaceID,
		SessionID:   principal.SessionID,
	})
}

func (h *Handler) listChannels(w http.ResponseWriter, r *http.Request) {
	principal, ok := dto.PrincipalFromContext(r.Context())
	if !ok {
		writeUnauthenticated(w, r)
		return
	}
	channels, err := h.Channels.GetContactChannels(r.Context(), principal)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": channels})
}

func (h *Handler) addChannel(w http.ResponseWriter, r *http.Request) {
	principal, ok := dto.PrincipalFromContext(r.Context())
	if !ok {
		writeUnauthenticated(w, r)
		return
	}
	var body struct {
		Kind    string `json:"kind"`
		Address string `json:"address"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	view, err := h.AddChannel.Handle(r.Context(), principal, body.Kind, body.Address)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidChannelKind),
			errors.Is(err, domain.ErrInvalidEmail),
			errors.Is(err, domain.ErrInvalidPhone):
			writeValidationError(w, r)
		case errors.Is(err, domain.ErrChannelExists):
			writeError(w, r, http.StatusConflict, "conflict", "contact channel already exists")
		default:
			writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (h *Handler) verifyChannel(w http.ResponseWriter, r *http.Request) {
	principal, ok := dto.PrincipalFromContext(r.Context())
	if !ok {
		writeUnauthenticated(w, r)
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if err := h.VerifyChannel.Handle(r.Context(), principal, r.PathValue("channelId"), body.Code); err != nil {
		switch {
		case errors.Is(err, domain.ErrChannelNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "contact channel not found")
		case errors.Is(err, domain.ErrInvalidCode), errors.Is(err, domain.ErrChannelCodeExpired):
			writeValidationError(w, r)
		default:
			writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"verified": true})
}

func (h *Handler) setChannelEnabled(w http.ResponseWriter, r *http.Request) {
	principal, ok := dto.PrincipalFromContext(r.Context())
	if !ok {
		writeUnauthenticated(w, r)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	view, err := h.SetChannelEnabled.Handle(r.Context(), principal, r.PathValue("channelId"), body.Enabled)
	if err != nil {
		if errors.Is(err, domain.ErrChannelNotFound) {
			writeError(w, r, http.StatusNotFound, "not_found", "contact channel not found")
			return
		}
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

package http

import (
	"errors"
	"net/http"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

func (h *Handler) resendChannel(w http.ResponseWriter, r *http.Request) {
	principal, ok := dto.PrincipalFromContext(r.Context())
	if !ok {
		writeUnauthenticated(w, r)
		return
	}
	if err := h.ResendChannel.Handle(r.Context(), principal, r.PathValue("channelId")); err != nil {
		switch {
		case errors.Is(err, domain.ErrChannelNotFound):
			writeError(w, r, http.StatusNotFound, "not_found", "contact channel not found")
		case errors.Is(err, domain.ErrChannelAlreadyVerified):
			writeError(w, r, http.StatusConflict, "conflict", "contact channel already verified")
		case errors.Is(err, domain.ErrRateLimited):
			w.Header().Set("Retry-After", "60")
			writeError(w, r, http.StatusTooManyRequests, "rate_limited", "wait before requesting another verification code")
		case errors.Is(err, domain.ErrSmsUnavailable):
			writeError(w, r, http.StatusServiceUnavailable, "sms_unavailable", "sms delivery is unavailable")
		case errors.Is(err, domain.ErrCodeDeliveryFailed):
			writeError(w, r, http.StatusBadGateway, "verification_send_failed", "verification code delivery failed")
		default:
			writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{})
}

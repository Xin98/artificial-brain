package server

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"regexp"

	"github.com/Xin98/artificial-brain/backend/internal/platform/observability"
)

var correlationIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func Correlation(next http.Handler) http.Handler {
	return correlationWithReader(next, rand.Reader)
}

func correlationWithReader(next http.Handler, entropy io.Reader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Correlation-ID")
		if !correlationIDPattern.MatchString(id) {
			var random [16]byte
			if _, err := io.ReadFull(entropy, random[:]); err != nil {
				writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
				return
			}
			id = hex.EncodeToString(random[:])
		}
		w.Header().Set("X-Correlation-ID", id)
		next.ServeHTTP(w, r.WithContext(observability.WithCorrelationID(r.Context(), id)))
	})
}

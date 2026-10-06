package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

func TestResendChannelRouteRequiresAuthentication(t *testing.T) {
	mux := newTestRouter(&Handler{})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/settings/contact-channels/c-1/resend", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want protected resend route 401", rr.Code)
	}
}

type fakeChannelResender struct {
	principal dto.Principal
	channelID string
	err       error
}

func (f *fakeChannelResender) Handle(_ context.Context, p dto.Principal, id string) error {
	f.principal, f.channelID = p, id
	return f.err
}

func TestResendChannelResponsesAndScope(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{{nil, 202}, {domain.ErrChannelNotFound, 404}, {domain.ErrChannelAlreadyVerified, 409}, {domain.ErrRateLimited, 429}, {domain.ErrSmsUnavailable, 503}, {domain.ErrCodeDeliveryFailed, 502}} {
		resender := &fakeChannelResender{err: test.err}
		mux := newTestRouter(&Handler{ResendChannel: resender})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, authenticatedRequest(http.MethodPost, "/api/v1/settings/contact-channels/c-1/resend", "{}"))
		if rr.Code != test.status {
			t.Fatalf("%v status=%d, want %d", test.err, rr.Code, test.status)
		}
		if resender.principal != testPrincipal || resender.channelID != "c-1" {
			t.Fatalf("scope=%#v / %s", resender.principal, resender.channelID)
		}
		if test.status == 429 && rr.Header().Get("Retry-After") != "60" {
			t.Fatal("cooldown missing Retry-After")
		}
	}
}

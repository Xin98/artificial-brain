package http

import (
	"context"
	"net/http"
	"testing"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
)

type pagingSessionLister struct {
	fakeSessionLister
	offset int
}

func (l *pagingSessionLister) HandlePage(_ context.Context, _, _ string, offset int) (dto.SessionListView, error) {
	l.offset = offset
	return l.view, l.err
}

type pagingHistory struct {
	fakeHistory
	before string
}

func (h *pagingHistory) HandlePage(_ context.Context, _, _, _, before string) (dto.SessionHistoryView, error) {
	h.before = before
	return h.view, h.err
}

type sessionConfirmer struct {
	fakeConfirmer
	session string
}

func (c *sessionConfirmer) HandleWithSession(_ context.Context, _, _, _, session string) (dto.MessageResponse, error) {
	c.session = session
	return c.response, c.err
}

func TestPagingAndConfirmationForwardSessionInputs(t *testing.T) {
	h := newTestHandler(&fakeProcessor{})
	lister, history, confirmer := &pagingSessionLister{}, &pagingHistory{}, &sessionConfirmer{}
	h.ListSessions, h.GetHistory, h.ConfirmAction = lister, history, confirmer
	serve(t, h, allowAuth, http.MethodGet, "/api/v1/conversation/sessions?offset=100", "")
	serve(t, h, allowAuth, http.MethodGet, "/api/v1/conversation/sessions/s-1/messages?before=201", "")
	serve(t, h, allowAuth, http.MethodPost, "/api/v1/confirmations/conf-1/confirm", `{"sessionId":"s-1"}`)
	if lister.offset != 100 || history.before != "201" || confirmer.session != "s-1" {
		t.Fatalf("forwarded offset/before/session = %d/%s/%s", lister.offset, history.before, confirmer.session)
	}
}

func TestPagingRejectsInvalidCursors(t *testing.T) {
	for _, url := range []string{"/api/v1/conversation/sessions?offset=-1", "/api/v1/conversation/sessions?offset=no", "/api/v1/conversation/sessions/s-1/messages?before=0", "/api/v1/conversation/sessions/s-1/messages?before=no", "/api/v1/conversation/sessions/s-1/messages?before=9223372036854775808"} {
		recorder := serve(t, newTestHandler(&fakeProcessor{}), allowAuth, http.MethodGet, url, "")
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s status = %d, want 422", url, recorder.Code)
		}
	}
}

func TestConfirmRejectsMalformedSessionBody(t *testing.T) {
	for _, body := range []string{`{"sessionId":3}`, `{"unexpected":true}`, `{`} {
		recorder := serve(t, newTestHandler(&fakeProcessor{}), allowAuth, http.MethodPost, "/api/v1/confirmations/conf-1/confirm", body)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Fatalf("body %q status = %d, want 422", body, recorder.Code)
		}
	}
}

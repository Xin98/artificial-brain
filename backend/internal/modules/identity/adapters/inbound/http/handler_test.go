package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/identity/domain"
)

type fakeLoginRequester struct {
	err        error
	identifier domain.LoginIdentifier
	// privateAdminPhone, when non-empty, mirrors the private-deployment gate
	// of command.RequestLoginChallengeHandler: every other phone is rejected
	// with domain.ErrRegistrationClosed before any other behavior.
	privateAdminPhone string
}

func (f *fakeLoginRequester) Handle(_ context.Context, identifier domain.LoginIdentifier) error {
	f.identifier = identifier
	if f.privateAdminPhone != "" && identifier.Phone != f.privateAdminPhone {
		return domain.ErrRegistrationClosed
	}
	return f.err
}

type fakeLoginVerifier struct {
	result dto.VerifyLoginChallengeResult
	err    error
	// privateAdminPhone, when non-empty, mirrors the private-deployment gate
	// of command.VerifyLoginChallengeHandler.
	privateAdminPhone string
}

func (f *fakeLoginVerifier) Handle(_ context.Context, identifier domain.LoginIdentifier, _ string) (dto.VerifyLoginChallengeResult, error) {
	if f.privateAdminPhone != "" && identifier.Phone != f.privateAdminPhone {
		return dto.VerifyLoginChallengeResult{}, domain.ErrRegistrationClosed
	}
	return f.result, f.err
}

type fakeLogout struct{ sessionID string }

func (f *fakeLogout) Handle(_ context.Context, sessionID string) error {
	f.sessionID = sessionID
	return nil
}

type fakeChannelAdder struct {
	view dto.ContactChannelView
	err  error
}

func (f *fakeChannelAdder) Handle(_ context.Context, _ dto.Principal, _, _ string) (dto.ContactChannelView, error) {
	return f.view, f.err
}

type fakeChannelVerifier struct{ err error }

func (f *fakeChannelVerifier) Handle(_ context.Context, _ dto.Principal, _, _ string) error {
	return f.err
}

type fakeChannelEnabledSetter struct {
	view dto.ContactChannelView
	err  error
}

func (f *fakeChannelEnabledSetter) Handle(_ context.Context, _ dto.Principal, _ string, _ bool) (dto.ContactChannelView, error) {
	return f.view, f.err
}

type fakeChannelsLister struct {
	views []dto.ContactChannelView
	err   error
}

func (f *fakeChannelsLister) GetContactChannels(_ context.Context, _ dto.Principal) ([]dto.ContactChannelView, error) {
	return f.views, f.err
}

var testPrincipal = dto.Principal{UserID: "u1", WorkspaceID: "w1", SessionID: "s1"}

// newTestRouter wires the handler with an auth middleware that accepts
// "valid-token" and injects testPrincipal.
func newTestRouter(h *Handler) *http.ServeMux {
	auth := NewAuthMiddleware(func(_ context.Context, token string) (dto.Principal, error) {
		if token != "valid-token" {
			return dto.Principal{}, http.ErrNoCookie
		}
		return testPrincipal, nil
	})
	mux := http.NewServeMux()
	RegisterRoutes(mux, auth, h)
	return mux
}

func authenticatedRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "valid-token"})
	return req
}

func TestRequestLogin(t *testing.T) {
	requester := &fakeLoginRequester{}
	mux := newTestRouter(&Handler{RequestLoginChallenge: requester})

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/request", strings.NewReader(`{"phone":"+8613800137000"}`)))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rr.Code)
	}
	if requester.identifier.Phone != "+8613800137000" {
		t.Fatalf("identifier = %#v", requester.identifier)
	}
}

func TestRequestLoginRejectsDualIdentifiers(t *testing.T) {
	handler := &Handler{RequestLoginChallenge: &fakeLoginRequester{}, SessionTTL: time.Hour}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/request",
		strings.NewReader(`{"phone":"+8613800138000","email":"admin@example.com"}`))
	recorder := httptest.NewRecorder()
	handler.requestLogin(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", recorder.Code)
	}
}

func TestRequestLoginRejectsMissingIdentifier(t *testing.T) {
	handler := &Handler{RequestLoginChallenge: &fakeLoginRequester{}, SessionTTL: time.Hour}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/request",
		strings.NewReader(`{}`))
	recorder := httptest.NewRecorder()
	handler.requestLogin(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", recorder.Code)
	}
}

func TestRequestLoginAcceptsEmailIdentifier(t *testing.T) {
	requester := &fakeLoginRequester{}
	handler := &Handler{RequestLoginChallenge: requester, SessionTTL: time.Hour}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/request",
		strings.NewReader(`{"email":"admin@example.com"}`))
	recorder := httptest.NewRecorder()
	handler.requestLogin(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", recorder.Code)
	}
	if requester.identifier.Email != "admin@example.com" || requester.identifier.Phone != "" {
		t.Fatalf("identifier = %#v", requester.identifier)
	}
}

func TestRequestLoginMapsSmsUnavailable(t *testing.T) {
	handler := &Handler{RequestLoginChallenge: &fakeLoginRequester{err: domain.ErrSmsUnavailable}, SessionTTL: time.Hour}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/request",
		strings.NewReader(`{"phone":"+8613800138000"}`))
	handler.requestLogin(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"sms_unavailable"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestRequestLoginMapsDeliveryFailure(t *testing.T) {
	handler := &Handler{RequestLoginChallenge: &fakeLoginRequester{
		err: fmt.Errorf("%w: dial", domain.ErrCodeDeliveryFailed),
	}, SessionTTL: time.Hour}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/request",
		strings.NewReader(`{"email":"admin@example.com"}`))
	handler.requestLogin(recorder, request)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"verification_send_failed"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestRequestLoginValidationAndRateLimit(t *testing.T) {
	mux := newTestRouter(&Handler{RequestLoginChallenge: &fakeLoginRequester{err: domain.ErrInvalidPhone}})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/request", strings.NewReader(`{"phone":"bad"}`)))
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid phone status = %d, want 422", rr.Code)
	}

	mux = newTestRouter(&Handler{RequestLoginChallenge: &fakeLoginRequester{err: domain.ErrRateLimited}})
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/request", strings.NewReader(`{"phone":"+8613800137000"}`)))
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limited status = %d, want 429", rr.Code)
	}
}

func TestVerifyLoginSetsCookie(t *testing.T) {
	verifier := &fakeLoginVerifier{result: dto.VerifyLoginChallengeResult{
		Token:     "session-token",
		Principal: testPrincipal,
		ExpiresAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
	}}
	mux := newTestRouter(&Handler{VerifyLoginChallenge: verifier, SessionTTL: 168 * time.Hour})

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/verify", strings.NewReader(`{"phone":"+8613800137000","code":"123456"}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	cookies := rr.Result().Cookies()
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == SessionCookieName {
			session = c
		}
	}
	if session == nil || session.Value != "session-token" || !session.HttpOnly || !session.Secure {
		t.Fatalf("session cookie = %#v", session)
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["userId"] != "u1" || body["workspaceId"] != "w1" {
		t.Fatalf("body = %#v", body)
	}
}

func TestVerifyLoginRejectsBadCode(t *testing.T) {
	mux := newTestRouter(&Handler{VerifyLoginChallenge: &fakeLoginVerifier{err: domain.ErrInvalidCode}})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/verify", strings.NewReader(`{"phone":"+8613800137000","code":"000000"}`)))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

// TestPrivateModeLoginGateMapsTo403RegistrationClosed proves the private-mode
// HTTP contract (Global Constraint 4): with PrivateAdminPhone set on both
// login handlers, any non-admin phone is mapped to 403 +
// {"code":"registration_closed"} on BOTH login routes, while the admin phone
// still gets 202 / proceeds.
func TestPrivateModeLoginGateMapsTo403RegistrationClosed(t *testing.T) {
	const adminPhone = "+8613800137000"
	const otherPhone = "+8613800139999"

	assertRegistrationClosed := func(t *testing.T, target, body string) {
		t.Helper()
		mux := newTestRouter(&Handler{
			RequestLoginChallenge: &fakeLoginRequester{privateAdminPhone: adminPhone},
			VerifyLoginChallenge:  &fakeLoginVerifier{privateAdminPhone: adminPhone},
		})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, target, strings.NewReader(body)))
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s status = %d, want 403", target, rr.Code)
		}
		var envelope struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&envelope); err != nil {
			t.Fatalf("%s decode error = %v", target, err)
		}
		if envelope.Code != "registration_closed" {
			t.Fatalf("%s code = %q, want registration_closed", target, envelope.Code)
		}
	}

	// Non-admin phone is refused on both routes.
	assertRegistrationClosed(t, "/api/v1/auth/login/request", `{"phone":"`+otherPhone+`"}`)
	assertRegistrationClosed(t, "/api/v1/auth/login/verify", `{"phone":"`+otherPhone+`","code":"123456"}`)

	// The admin phone still gets 202 on the request route.
	requester := &fakeLoginRequester{privateAdminPhone: adminPhone}
	mux := newTestRouter(&Handler{RequestLoginChallenge: requester})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/request", strings.NewReader(`{"phone":"`+adminPhone+`"}`)))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("admin request status = %d, want 202", rr.Code)
	}
	if requester.identifier.Phone != adminPhone {
		t.Fatalf("admin request identifier = %#v", requester.identifier)
	}

	// The admin phone still proceeds on the verify route.
	verifier := &fakeLoginVerifier{
		privateAdminPhone: adminPhone,
		result: dto.VerifyLoginChallengeResult{
			Token:     "session-token",
			Principal: testPrincipal,
			ExpiresAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
		},
	}
	mux = newTestRouter(&Handler{VerifyLoginChallenge: verifier, SessionTTL: 168 * time.Hour})
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/verify", strings.NewReader(`{"phone":"`+adminPhone+`","code":"123456"}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("admin verify status = %d, want 200", rr.Code)
	}
}

func TestProtectedRoutesRequireAuth(t *testing.T) {
	mux := newTestRouter(&Handler{Channels: &fakeChannelsLister{}})
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/settings/contact-channels", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil),
	} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401", req.Method, req.URL.Path, rr.Code)
		}
	}
}

func TestSessionReturnsPrincipal(t *testing.T) {
	mux := newTestRouter(&Handler{})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, authenticatedRequest(http.MethodGet, "/api/v1/auth/session", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var view dto.SessionView
	if err := json.NewDecoder(rr.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view.UserID != "u1" || view.WorkspaceID != "w1" || view.SessionID != "s1" {
		t.Fatalf("view = %#v", view)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	logout := &fakeLogout{}
	mux := newTestRouter(&Handler{Logout: logout})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, authenticatedRequest(http.MethodPost, "/api/v1/auth/logout", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if logout.sessionID != "s1" {
		t.Fatalf("logout sessionID = %q", logout.sessionID)
	}
	var cleared *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == SessionCookieName {
			cleared = c
		}
	}
	if cleared == nil || cleared.MaxAge != -1 {
		t.Fatalf("expected cleared cookie, got %#v", cleared)
	}
}

func TestAddChannelAndConflicts(t *testing.T) {
	view := dto.ContactChannelView{ID: "c1", Kind: "email", Address: "user@example.com"}
	mux := newTestRouter(&Handler{AddChannel: &fakeChannelAdder{view: view}})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, authenticatedRequest(http.MethodPost, "/api/v1/settings/contact-channels", `{"kind":"email","address":"user@example.com"}`))
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rr.Code)
	}

	mux = newTestRouter(&Handler{AddChannel: &fakeChannelAdder{err: domain.ErrChannelExists}})
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, authenticatedRequest(http.MethodPost, "/api/v1/settings/contact-channels", `{"kind":"email","address":"user@example.com"}`))
	if rr.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, want 409", rr.Code)
	}

	mux = newTestRouter(&Handler{AddChannel: &fakeChannelAdder{err: domain.ErrInvalidEmail}})
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, authenticatedRequest(http.MethodPost, "/api/v1/settings/contact-channels", `{"kind":"email","address":"bad"}`))
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid status = %d, want 422", rr.Code)
	}
}

func TestVerifyChannel(t *testing.T) {
	mux := newTestRouter(&Handler{VerifyChannel: &fakeChannelVerifier{}})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, authenticatedRequest(http.MethodPost, "/api/v1/settings/contact-channels/c1/verify", `{"code":"222333"}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	mux = newTestRouter(&Handler{VerifyChannel: &fakeChannelVerifier{err: domain.ErrChannelNotFound}})
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, authenticatedRequest(http.MethodPost, "/api/v1/settings/contact-channels/missing/verify", `{"code":"222333"}`))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d, want 404", rr.Code)
	}
}

func TestSetChannelEnabled(t *testing.T) {
	view := dto.ContactChannelView{ID: "c1", Enabled: false}
	mux := newTestRouter(&Handler{SetChannelEnabled: &fakeChannelEnabledSetter{view: view}})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, authenticatedRequest(http.MethodPatch, "/api/v1/settings/contact-channels/c1", `{"enabled":false}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	mux = newTestRouter(&Handler{SetChannelEnabled: &fakeChannelEnabledSetter{err: domain.ErrChannelNotFound}})
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, authenticatedRequest(http.MethodPatch, "/api/v1/settings/contact-channels/missing", `{"enabled":true}`))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d, want 404", rr.Code)
	}
}

func TestListChannels(t *testing.T) {
	views := []dto.ContactChannelView{{ID: "c1", Kind: "email", Address: "a@example.com", Verified: true, Enabled: true}}
	mux := newTestRouter(&Handler{Channels: &fakeChannelsLister{views: views}})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, authenticatedRequest(http.MethodGet, "/api/v1/settings/contact-channels", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body struct {
		Channels []dto.ContactChannelView `json:"channels"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Channels) != 1 || body.Channels[0].ID != "c1" {
		t.Fatalf("channels = %#v", body.Channels)
	}
}

func TestDevInboxHandler(t *testing.T) {
	store := &fakeDevInboxStore{messages: []DevInboxMessage{
		{Address: "+8613800137000", Channel: "sms", Purpose: "login", Code: "123456", CreatedAt: time.Now()},
	}}
	handler := NewDevInboxHandler(store)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/dev/sms-inbox?address=%2B8613800137000", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body struct {
		Messages []DevInboxMessage `json:"messages"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) != 1 || body.Messages[0].Code != "123456" {
		t.Fatalf("messages = %#v", body.Messages)
	}

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/dev/sms-inbox", nil))
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing address status = %d, want 422", rr.Code)
	}
}

type fakeDevInboxStore struct{ messages []DevInboxMessage }

func (f *fakeDevInboxStore) LatestByAddress(_ context.Context, _ string, _ int) ([]DevInboxMessage, error) {
	return f.messages, nil
}

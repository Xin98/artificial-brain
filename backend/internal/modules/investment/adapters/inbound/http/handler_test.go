package http

import (
	"context"
	"errors"
	identity "github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type createAccountStub struct {
	scope domain.Scope
	calls int
}

func (s *createAccountStub) Handle(_ context.Context, r dto.CreateAccountRequest) (dto.AccountView, error) {
	s.scope = r.Scope
	s.calls++
	return dto.AccountView{ID: "result", Cash: dto.CashView{Available: "100000.00"}}, nil
}

type accountQueryStub struct{ err error }

func (s accountQueryStub) Handle(context.Context, domain.Scope, string) (dto.AccountView, error) {
	return dto.AccountView{}, s.err
}
func TestInvestmentHTTPAuthScopeAndStrictBody(t *testing.T) {
	create := &createAccountStub{}
	handler := &Handler{CreateAccount: create}
	mux := http.NewServeMux()
	RegisterRoutes(mux, func(h http.Handler) http.Handler { return h }, handler)
	req := httptest.NewRequest("POST", "/api/v1/investment/accounts", strings.NewReader("{}"))
	req.Header.Set("Idempotency-Key", "test")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	principal := identity.Principal{UserID: "22222222-2222-4222-8222-222222222222", WorkspaceID: "11111111-1111-4111-8111-111111111111"}
	for _, body := range []string{`{"ownerUserId":"other"}`, `{"initialCash":100000}`, `{} {}`, `null`, strings.Repeat(" ", 65537) + "{}"} {
		req = httptest.NewRequest("POST", "/api/v1/investment/accounts", strings.NewReader(body))
		req.Header.Set("Idempotency-Key", "test")
		req = req.WithContext(identity.WithPrincipal(req.Context(), principal))
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != 422 {
			t.Fatal(body[:min(len(body), 50)], w.Code)
		}
	}
	req = httptest.NewRequest("POST", "/api/v1/investment/accounts", strings.NewReader(`{"initialCash":"100000.00"}`))
	req.Header.Set("Idempotency-Key", "same-intent")
	req = req.WithContext(identity.WithPrincipal(req.Context(), principal))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 201 || create.calls != 1 || create.scope.OwnerUserID != principal.UserID {
		t.Fatal(w.Code, create)
	}
}
func TestInvestmentServiceFailureNotEmptyAccount(t *testing.T) {
	handler := &Handler{Account: accountQueryStub{err: errors.New("database unavailable")}}
	mux := http.NewServeMux()
	RegisterRoutes(mux, func(h http.Handler) http.Handler { return h }, handler)
	r := httptest.NewRequest("GET", "/api/v1/investment/accounts/33333333-3333-4333-8333-333333333333", nil)
	r = r.WithContext(identity.WithPrincipal(r.Context(), identity.Principal{WorkspaceID: "w", UserID: "u"}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "cash") {
		t.Fatal(w.Code, w.Body.String())
	}
}
